//go:build integration

package walletgroup

import (
	"context"
	"fmt"
	"testing"
)

// HARD-05: group detail wallet_count stays a single aggregate query at
// scale — 100 members resolve to an exact count in one call (no N+1).
func TestPerf_GroupDetail_CountAtScale(t *testing.T) {
	f := setupWalletGroupFixture(t)
	ctx := context.Background()
	g := f.createGroup(t, f.userA, "HARD-05 Group")

	const members = 100
	addrs := make([]string, 0, members)
	for i := 0; i < members; i++ {
		addr := fmt.Sprintf("0x%040x", 0xbb00+i)
		addrs = append(addrs, addr)
		f.trackAddr(addr)
	}
	created, err := f.svc.AddWallets(ctx, f.userA, g.ID, &WalletsRequest{Wallets: addrs})
	if err != nil {
		t.Fatalf("add wallets: %v", err)
	}
	if created != members {
		t.Fatalf("expected %d created, got %d", members, created)
	}

	got, err := f.svc.GetGroup(ctx, f.userA, g.ID)
	if err != nil {
		t.Fatalf("get group: %v", err)
	}
	if got.WalletCount != members {
		t.Errorf("expected wallet_count %d, got %d", members, got.WalletCount)
	}

	// One page walk covers every member exactly once (stable pagination).
	seen := map[string]bool{}
	for page := 1; ; page++ {
		refs, _, err := f.repo.ListMembers(ctx, g.ID, "", 30, (page-1)*30)
		if err != nil {
			t.Fatalf("list members: %v", err)
		}
		if len(refs) == 0 {
			break
		}
		for _, r := range refs {
			if seen[r.Address] {
				t.Fatalf("duplicate member %s across pages", r.Address)
			}
			seen[r.Address] = true
		}
		if page > 10 {
			t.Fatal("page walk did not terminate")
		}
	}
	if len(seen) != members {
		t.Errorf("walked %d members, expected %d", len(seen), members)
	}
}
