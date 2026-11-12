package store

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// AppAccess is what one person may do on the apps of one organization.
// Role is the one place that decides it; everything that reaches an app
// (routes, lists, streams) asks it.
type AppAccess struct {
	OrgID int64
	// OrgRole is the person's role in the organization ("" when not a
	// member: then no app of it is theirs).
	OrgRole string
	// listed are the organization's apps with a list, by name, and the
	// person's role on each ("" when the list leaves them out).
	listed map[string]string
}

// Role is the person's role on an app: the organization's administrators
// are always administrators; on an app without a list, the organization's
// role, as before lists existed; on an app with a list, the role the list
// gives them, or none (the app does not exist for them). Another
// organization's app is never theirs.
func (a AppAccess) Role(app *App) string {
	if app == nil || app.OrgID != a.OrgID {
		return ""
	}
	return a.RoleOn(app.Name)
}

// RoleOn is Role for an app of the organization known by its name (from a
// list already narrowed to the organization).
func (a AppAccess) RoleOn(name string) string {
	switch {
	case a.OrgRole == "":
		return ""
	case a.OrgRole == MemberAdmin:
		return MemberAdmin
	}
	if role, ok := a.listed[name]; ok {
		return role
	}
	return a.OrgRole
}

// Sees says whether something about the app named (of the organization)
// may be shown: always for "" (about no app).
func (a AppAccess) Sees(name string) bool { return name == "" || a.RoleOn(name) != "" }

// Hidden are the organization's apps the person may not see.
func (a AppAccess) Hidden() []string {
	out := []string{}
	if a.OrgRole == MemberAdmin {
		return out
	}
	for name, role := range a.listed {
		if role == "" {
			out = append(out, name)
		}
	}
	return out
}

// Narrowed is the access with every role capped at most (a demo link's
// guest never does more than read).
func (a AppAccess) Narrowed(most string) AppAccess {
	capped := func(r string) string {
		if r != "" && RoleRank(r) > RoleRank(most) {
			return most
		}
		return r
	}
	out := AppAccess{OrgID: a.OrgID, OrgRole: capped(a.OrgRole), listed: map[string]string{}}
	for n, r := range a.listed {
		out.listed[n] = capped(r)
	}
	return out
}

// AppAccessOf reads what login may do on the organization's apps, given
// their role in it (as the caller judged it: "" when not a member).
func (s *Store) AppAccessOf(ctx context.Context, orgID int64, login, orgRole string) (AppAccess, error) {
	acc := AppAccess{OrgID: orgID, OrgRole: orgRole, listed: map[string]string{}}
	if orgRole == "" || orgRole == MemberAdmin {
		return acc, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT a.name, COALESCE(m.role, '') FROM apps a
		LEFT JOIN app_members m ON m.app_id = a.id AND lower(m.login) = lower($2)
		WHERE a.org_id = $1 AND a.access_list`, orgID, login)
	if err != nil {
		return acc, err
	}
	defer rows.Close()
	for rows.Next() {
		var name, role string
		if err := rows.Scan(&name, &role); err != nil {
			return acc, err
		}
		acc.listed[name] = role
	}
	return acc, rows.Err()
}

// AppList is who enters an app: whether it has a list, and the people on
// it with their role for the app.
type AppList struct {
	List    bool     `json:"list"`
	Members []Member `json:"members"`
}

// ErrNotInOrg is a person who is not in the app's organization.
var ErrNotInOrg = errors.New("that person is not in this organization; add them on the Members page first")

// ErrOrgAdmin is an organization's administrator, who always enters.
var ErrOrgAdmin = errors.New("the organization's administrators always enter every app")

// AppMembers is an app's list (people taken out of the organization are
// left out: they no longer count).
func (s *Store) AppMembers(ctx context.Context, app *App) (AppList, error) {
	out := AppList{Members: []Member{}}
	if err := s.pool.QueryRow(ctx, `SELECT access_list FROM apps WHERE id = $1`, app.ID).Scan(&out.List); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return out, ErrNotFound
		}
		return out, err
	}
	rows, err := s.pool.Query(ctx, `SELECT m.login, m.role, m.added_by, m.created_at FROM app_members m
		JOIN org_members o ON o.org_id = $2 AND lower(o.login) = lower(m.login)
		WHERE m.app_id = $1
		ORDER BY CASE m.role WHEN 'admin' THEN 0 WHEN 'collaborator' THEN 1 ELSE 2 END, lower(m.login)`, app.ID, app.OrgID)
	if err != nil {
		return out, err
	}
	members, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Member, error) {
		var m Member
		err := r.Scan(&m.Login, &m.Role, &m.AddedBy, &m.CreatedAt)
		return m, err
	})
	if err != nil {
		return out, err
	}
	if members != nil {
		out.Members = members
	}
	return out, nil
}

// SetAppList turns an app's list on (only the organization's
// administrators enter until people are added) or off (everyone of the
// organization enters with their role, and the list is forgotten).
func (s *Store) SetAppList(ctx context.Context, appID int64, on bool) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE apps SET access_list = $2 WHERE id = $1`, appID, on); err != nil {
			return err
		}
		if on {
			return nil
		}
		_, err := tx.Exec(ctx, `DELETE FROM app_members WHERE app_id = $1`, appID)
		return err
	})
}

// SetAppMember gives a person of the organization a role on the app, and
// turns the app's list on if it was off.
func (s *Store) SetAppMember(ctx context.Context, app *App, login, role, by string) (Member, error) {
	login = strings.TrimSpace(login)
	orgRole, err := s.MemberRole(ctx, app.OrgID, login)
	if errors.Is(err, ErrNotFound) {
		return Member{}, ErrNotInOrg
	}
	if err != nil {
		return Member{}, err
	}
	if orgRole == MemberAdmin {
		return Member{}, ErrOrgAdmin
	}
	m := Member{Login: login, Role: role, AddedBy: by}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE apps SET access_list = true WHERE id = $1`, app.ID); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `INSERT INTO app_members (app_id, login, role, added_by) VALUES ($1, $2, $3, $4)
			ON CONFLICT (app_id, lower(login)) DO UPDATE SET role = EXCLUDED.role
			RETURNING login, added_by, created_at`, app.ID, login, role, by).Scan(&m.Login, &m.AddedBy, &m.CreatedAt)
	})
	return m, err
}

// RemoveAppMember takes a person off the app's list: they no longer see it
// (unless they become an administrator of the organization).
func (s *Store) RemoveAppMember(ctx context.Context, appID int64, login string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM app_members WHERE app_id = $1 AND lower(login) = lower($2)`, appID, login)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// forgetAppMembers drops a person from the lists of an organization's
// apps (they left it): coming back, they start with no app with a list.
func forgetAppMembers(ctx context.Context, tx pgx.Tx, orgID int64, login string) error {
	_, err := tx.Exec(ctx, `DELETE FROM app_members WHERE lower(login) = lower($2)
		AND app_id IN (SELECT id FROM apps WHERE org_id = $1)`, orgID, login)
	return err
}
