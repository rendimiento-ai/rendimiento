package store

import (
	"context"
	"testing"
	"time"
)

// A key changed twice keeps one row: the last change, who made it and its
// expiry, with its version up by one each time.
func TestLlaves(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	if l, err := s.Llaves(ctx); err != nil || len(l) != 0 {
		t.Fatalf("none yet: %v %v", l, err)
	}
	vence := time.Date(2027, 11, 20, 0, 0, 0, 0, time.UTC)
	l, err := s.AnotarLlave(ctx, "cloudflare", "p0dxD", &vence)
	if err != nil || l.Version != 1 || l.VenceEn == nil || !l.VenceEn.Equal(vence) {
		t.Fatalf("first change: %+v %v", l, err)
	}
	if l, err = s.AnotarLlave(ctx, "cloudflare", "ana", nil); err != nil || l.Version != 2 || l.CambiadaPor != "ana" || l.VenceEn != nil {
		t.Fatalf("second change: %+v %v", l, err)
	}
	if _, err := s.AnotarLlave(ctx, "ia", "ana", nil); err != nil {
		t.Fatal(err)
	}
	all, err := s.Llaves(ctx)
	if err != nil || len(all) != 2 || all["cloudflare"].Version != 2 || all["ia"].Version != 1 {
		t.Fatalf("both: %+v %v", all, err)
	}
}
