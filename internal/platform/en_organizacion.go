package platform

import (
	"context"
	"errors"
	"net/url"
	"strings"

	. "github.com/rendimiento-ai/rendimiento/internal/i18n" //nolint:revive // M marks messages for people
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// allowedInOrg checks what an app may be in its organization, beyond what
// rendimiento.yaml itself allows (spec.Validate): it is checked when the
// app is created and on every push that brings its rendimiento.yaml.
//
//   - sharedNamespace is for the platform's organization: it deploys into a
//     namespace that already exists and that rendimiento does not own, and
//     those belong to the cluster's operators.
//   - No organization takes an address an app of another organization
//     claims, even before that app has an ingress (the controller refuses a
//     host already served, but only once it is served); and no app of
//     another organization takes the platform's own address.
//
// appID is the app's own (0 while it is being created), so its addresses
// are not counted against it.
func (p *Platform) allowedInOrg(ctx context.Context, orgID, appID int64, sp spec.Spec) error {
	if orgID == 0 {
		orgID = store.DefaultOrg
	}
	if orgID != store.DefaultOrg && sp.SharedNamespace {
		return errors.New(M("sharedNamespace is for the platform's organization: an app of another organization always has a namespace of its own"))
	}
	var hosts []string
	for _, svc := range sp.Services {
		hosts = append(hosts, svc.Hosts()...)
	}
	if len(hosts) == 0 {
		return nil
	}
	if u, err := url.Parse(p.Config.BaseURL); err == nil && u.Hostname() != "" && orgID != store.DefaultOrg {
		for _, h := range hosts {
			if strings.EqualFold(h, u.Hostname()) {
				return errors.New(M("%s is the platform's own address; choose another", h))
			}
		}
	}
	apps, err := p.Store.ListApps(ctx)
	if err != nil {
		return err
	}
	for _, a := range apps {
		other := a.OrgID
		if other == 0 {
			other = store.DefaultOrg
		}
		if a.ID == appID || other == orgID {
			continue
		}
		for _, svc := range a.Spec.Services {
			for _, theirs := range svc.Hosts() {
				for _, h := range hosts {
					if strings.EqualFold(h, theirs) {
						// Neither the app nor its organization is named.
						return errors.New(M("the address %s is taken by an app of another organization; choose another", h))
					}
				}
			}
		}
	}
	return nil
}
