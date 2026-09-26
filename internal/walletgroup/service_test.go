package walletgroup

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qwerty7415963/go_be_arbitrage/internal/domain"
)

type mockRepo struct {
	createGroupFn   func(ctx context.Context, g *Group) error
	getGroupByIDFn  func(ctx context.Context, id uuid.UUID) (*Group, error)
	listGroupsFn    func(ctx context.Context, userID uuid.UUID) ([]*Group, error)
	updateGroupFn   func(ctx context.Context, g *Group) error
	deleteGroupFn   func(ctx context.Context, id, userID uuid.UUID) (int64, error)
	addMembersFn    func(ctx context.Context, groupID, addedBy uuid.UUID, items []walletItem) (int64, error)
	removeMembersFn func(ctx context.Context, groupID uuid.UUID, items []walletItem) (int64, error)
	listMembersFn   func(ctx context.Context, groupID uuid.UUID, search string, limit, offset int) ([]*WalletRef, int64, error)
}

func (m *mockRepo) CreateGroup(ctx context.Context, g *Group) error {
	if m.createGroupFn != nil {
		return m.createGroupFn(ctx, g)
	}
	return errors.New("createGroupFn not set")
}

func (m *mockRepo) GetGroupByID(ctx context.Context, id uuid.UUID) (*Group, error) {
	if m.getGroupByIDFn != nil {
		return m.getGroupByIDFn(ctx, id)
	}
	return nil, errors.New("getGroupByIDFn not set")
}

func (m *mockRepo) ListGroups(ctx context.Context, userID uuid.UUID) ([]*Group, error) {
	if m.listGroupsFn != nil {
		return m.listGroupsFn(ctx, userID)
	}
	return nil, errors.New("listGroupsFn not set")
}

func (m *mockRepo) UpdateGroup(ctx context.Context, g *Group) error {
	if m.updateGroupFn != nil {
		return m.updateGroupFn(ctx, g)
	}
	return errors.New("updateGroupFn not set")
}

func (m *mockRepo) DeleteGroup(ctx context.Context, id, userID uuid.UUID) (int64, error) {
	if m.deleteGroupFn != nil {
		return m.deleteGroupFn(ctx, id, userID)
	}
	return 0, errors.New("deleteGroupFn not set")
}

func (m *mockRepo) AddMembersTx(ctx context.Context, groupID, addedBy uuid.UUID, items []walletItem) (int64, error) {
	if m.addMembersFn != nil {
		return m.addMembersFn(ctx, groupID, addedBy, items)
	}
	return 0, errors.New("addMembersFn not set")
}

func (m *mockRepo) RemoveMembers(ctx context.Context, groupID uuid.UUID, items []walletItem) (int64, error) {
	if m.removeMembersFn != nil {
		return m.removeMembersFn(ctx, groupID, items)
	}
	return 0, errors.New("removeMembersFn not set")
}

func (m *mockRepo) ListMembers(ctx context.Context, groupID uuid.UUID, search string, limit, offset int) ([]*WalletRef, int64, error) {
	if m.listMembersFn != nil {
		return m.listMembersFn(ctx, groupID, search, limit, offset)
	}
	return nil, 0, errors.New("listMembersFn not set")
}

const validHexAddr = "0xABCDEF1234567890ABCDEF1234567890ABCDEF12"

func appErrCode(t *testing.T, err error) domain.ErrorCode {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	return appErr.Code
}

// ─── GRP-U-01: address normalization ────────────────────────────

func TestNormalizeAddress_ValidTrimsAndLowercases(t *testing.T) {
	got, err := NormalizeAddress("  " + validHexAddr + " ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := strings.ToLower(validHexAddr)
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// ─── GRP-U-02: invalid address → WALLET-002 ─────────────────────

func TestNormalizeAddress_Invalid(t *testing.T) {
	cases := []string{
		"0x123",           // too short
		"nothexatall",     // non-hex
		"0xZZZZ" + "X",    // invalid chars
		"",                // blank
		validHexAddr[:40], // missing prefix, wrong length
	}
	for _, addr := range cases {
		if _, err := NormalizeAddress(addr); err == nil {
			t.Errorf("expected WALLET-002 for %q", addr)
		} else if code := appErrCode(t, err); code != domain.ErrCodeWalletInvalidAddress {
			t.Errorf("expected WALLET-002 for %q, got %s", addr, code)
		}
	}
}

// ─── GRP-U-03: chain normalization → same identity ──────────────

func TestNormalizeChain_SameCanonicalChainID(t *testing.T) {
	a, err := NormalizeChain("1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := NormalizeChain("0x1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c, err := NormalizeChain("  1  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a != b || b != c {
		t.Errorf("expected same canonical chain, got %q %q %q", a, b, c)
	}
	if a != "1" {
		t.Errorf("expected canonical chain \"1\", got %q", a)
	}
}

func TestNormalizeChain_Invalid(t *testing.T) {
	if _, err := NormalizeChain("!!!"); err == nil {
		t.Error("expected WALLET-003 for invalid chain")
	} else if code := appErrCode(t, err); code != domain.ErrCodeWalletUnsupportedChain {
		t.Errorf("expected WALLET-003, got %s", code)
	}
}

func TestNormalizeChain_EmptyDefaultsToEVM(t *testing.T) {
	got, err := NormalizeChain("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "evm" {
		t.Errorf("expected \"evm\", got %q", got)
	}
}

// ─── GRP-U-04: group name validation → COMMON-902 on name ───────

func TestValidateGroupName_Blank(t *testing.T) {
	for _, name := range []string{"", "   ", "\t\n"} {
		_, fieldErr := ValidateGroupName(name)
		if fieldErr == nil {
			t.Errorf("expected field error for %q", name)
			continue
		}
		if fieldErr.Field != "name" {
			t.Errorf("expected field \"name\", got %q", fieldErr.Field)
		}
		if fieldErr.Code != string(domain.ErrCodeValidation) {
			t.Errorf("expected COMMON-902, got %s", fieldErr.Code)
		}
	}
}

func TestValidateGroupName_ValidTrims(t *testing.T) {
	got, fieldErr := ValidateGroupName("  Smart Money  ")
	if fieldErr != nil {
		t.Fatalf("unexpected field error: %v", fieldErr)
	}
	if got != "Smart Money" {
		t.Errorf("expected trimmed name, got %q", got)
	}
}

// ─── GRP-U-05: CreateGroup success ──────────────────────────────

func TestService_CreateGroup_Success(t *testing.T) {
	repo := &mockRepo{
		createGroupFn: func(ctx context.Context, g *Group) error { return nil },
	}
	svc := NewService(repo)
	userID := uuid.New()

	g, err := svc.CreateGroup(context.Background(), userID, &CreateGroupRequest{Name: "  Smart Money  "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.ID == uuid.Nil {
		t.Error("expected group id to be set")
	}
	if g.UserID != userID {
		t.Errorf("expected owner %v, got %v", userID, g.UserID)
	}
	if g.Name != "Smart Money" {
		t.Errorf("expected trimmed name, got %q", g.Name)
	}
}

// ─── GRP-U-06: duplicate name, same user → GROUP-002 ────────────

func TestService_CreateGroup_DuplicateNameSameUser(t *testing.T) {
	repo := &mockRepo{
		createGroupFn: func(ctx context.Context, g *Group) error { return ErrDuplicateName },
	}
	svc := NewService(repo)

	_, err := svc.CreateGroup(context.Background(), uuid.New(), &CreateGroupRequest{Name: "Smart Money"})
	if code := appErrCode(t, err); code != domain.ErrCodeGroupDuplicate {
		t.Errorf("expected GROUP-002, got %s", code)
	}
}

// ─── GRP-U-07: duplicate name, different users → allowed ────────

func TestService_CreateGroup_DuplicateNameDifferentUsers(t *testing.T) {
	var createdFor []uuid.UUID
	repo := &mockRepo{
		createGroupFn: func(ctx context.Context, g *Group) error {
			createdFor = append(createdFor, g.UserID)
			return nil
		},
	}
	svc := NewService(repo)

	if _, err := svc.CreateGroup(context.Background(), uuid.New(), &CreateGroupRequest{Name: "Smart Money"}); err != nil {
		t.Fatalf("first user create failed: %v", err)
	}
	if _, err := svc.CreateGroup(context.Background(), uuid.New(), &CreateGroupRequest{Name: "Smart Money"}); err != nil {
		t.Fatalf("second user create should be allowed: %v", err)
	}
	if len(createdFor) != 2 || createdFor[0] == createdFor[1] {
		t.Errorf("expected two distinct users, got %v", createdFor)
	}
}

// ─── GRP-U-08: IsGroupOwnedBy ───────────────────────────────────

func TestService_IsGroupOwnedBy(t *testing.T) {
	owner := uuid.New()
	groupID := uuid.New()
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			if id != groupID {
				return nil, pgx.ErrNoRows
			}
			return &Group{ID: groupID, UserID: owner, Name: "G"}, nil
		},
	}
	svc := NewService(repo)

	if !svc.IsGroupOwnedBy(context.Background(), groupID, owner) {
		t.Error("expected true for owner")
	}
	if svc.IsGroupOwnedBy(context.Background(), groupID, uuid.New()) {
		t.Error("expected false for non-owner")
	}
	if svc.IsGroupOwnedBy(context.Background(), uuid.New(), owner) {
		t.Error("expected false for unknown group")
	}
}

// ─── GRP-U-09: DeleteGroup removes group, wallet rows untouched ─

func TestService_DeleteGroup_RemovesGroupOnly(t *testing.T) {
	owner := uuid.New()
	groupID := uuid.New()
	var deletedID, deletedUser uuid.UUID
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return &Group{ID: groupID, UserID: owner, Name: "G"}, nil
		},
		deleteGroupFn: func(ctx context.Context, id, userID uuid.UUID) (int64, error) {
			deletedID, deletedUser = id, userID
			return 1, nil
		},
	}
	svc := NewService(repo)

	if err := svc.DeleteGroup(context.Background(), owner, groupID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deletedID != groupID || deletedUser != owner {
		t.Errorf("expected delete (%v, %v), got (%v, %v)", groupID, owner, deletedID, deletedUser)
	}
	// RepositoryInterface exposes no wallet-table mutation other than
	// memberships (group cascade is DB-side), so tracked_wallets cannot be
	// touched from here (BR-10).
}

func TestService_DeleteGroup_NotFoundAfterRace(t *testing.T) {
	owner := uuid.New()
	groupID := uuid.New()
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return &Group{ID: groupID, UserID: owner, Name: "G"}, nil
		},
		deleteGroupFn: func(ctx context.Context, id, userID uuid.UUID) (int64, error) {
			return 0, nil
		},
	}
	svc := NewService(repo)

	if code := appErrCode(t, svc.DeleteGroup(context.Background(), owner, groupID)); code != domain.ErrCodeGroupNotFound {
		t.Errorf("expected GROUP-001, got %s", code)
	}
}

// ─── GRP-U-10: AddWallet bulk ───────────────────────────────────

func TestService_AddWallets_Bulk(t *testing.T) {
	owner := uuid.New()
	groupID := uuid.New()
	var gotItems []walletItem
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return &Group{ID: groupID, UserID: owner}, nil
		},
		addMembersFn: func(ctx context.Context, g, by uuid.UUID, items []walletItem) (int64, error) {
			gotItems = items
			return int64(len(items)), nil
		},
	}
	svc := NewService(repo)

	w1, w2, w3 := uuid.New(), uuid.New(), uuid.New()
	created, err := svc.AddWallets(context.Background(), owner, groupID, &WalletsRequest{
		Wallets: []string{w1.String(), w2.String(), w3.String()},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created != 3 {
		t.Errorf("expected 3 created, got %d", created)
	}
	if len(gotItems) != 3 {
		t.Fatalf("expected 3 items, got %d", len(gotItems))
	}
	for i, it := range gotItems {
		if !it.isID {
			t.Errorf("item %d: expected ID item", i)
		}
	}
}

// ─── GRP-U-11: duplicate add → idempotent success ───────────────

func TestService_AddWallets_IdempotentReAdd(t *testing.T) {
	owner := uuid.New()
	groupID := uuid.New()
	calls := 0
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return &Group{ID: groupID, UserID: owner}, nil
		},
		addMembersFn: func(ctx context.Context, g, by uuid.UUID, items []walletItem) (int64, error) {
			calls++
			if calls == 1 {
				return 1, nil // first add inserts
			}
			return 0, nil // ON CONFLICT DO NOTHING
		},
	}
	svc := NewService(repo)

	wallet := uuid.New().String()
	created, err := svc.AddWallets(context.Background(), owner, groupID, &WalletsRequest{Wallets: []string{wallet}})
	if err != nil || created != 1 {
		t.Fatalf("first add: created=%d err=%v", created, err)
	}
	created, err = svc.AddWallets(context.Background(), owner, groupID, &WalletsRequest{Wallets: []string{wallet}})
	if err != nil {
		t.Fatalf("re-add must be idempotent success, got %v", err)
	}
	if created != 0 {
		t.Errorf("expected 0 new memberships on re-add, got %d", created)
	}
}

// ─── GRP-U-12: unknown wallet → WALLET-001 ──────────────────────

func TestService_AddWallets_UnknownWallet(t *testing.T) {
	owner := uuid.New()
	groupID := uuid.New()
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return &Group{ID: groupID, UserID: owner}, nil
		},
		addMembersFn: func(ctx context.Context, g, by uuid.UUID, items []walletItem) (int64, error) {
			return 0, ErrWalletNotFound
		},
	}
	svc := NewService(repo)

	_, err := svc.AddWallets(context.Background(), owner, groupID, &WalletsRequest{
		Wallets: []string{uuid.New().String()},
	})
	if code := appErrCode(t, err); code != domain.ErrCodeWalletNotFound {
		t.Errorf("expected WALLET-001, got %s", code)
	}
}

func TestService_AddWallets_InvalidAddress(t *testing.T) {
	owner := uuid.New()
	groupID := uuid.New()
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return &Group{ID: groupID, UserID: owner}, nil
		},
	}
	svc := NewService(repo)

	_, err := svc.AddWallets(context.Background(), owner, groupID, &WalletsRequest{
		Wallets: []string{"not-a-wallet"},
	})
	if code := appErrCode(t, err); code != domain.ErrCodeWalletInvalidAddress {
		t.Errorf("expected WALLET-002, got %s", code)
	}
}

// ─── GRP-U-13: remove absent wallet → idempotent success ────────

func TestService_RemoveWallets_AbsentIsNoop(t *testing.T) {
	owner := uuid.New()
	groupID := uuid.New()
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return &Group{ID: groupID, UserID: owner}, nil
		},
		removeMembersFn: func(ctx context.Context, g uuid.UUID, items []walletItem) (int64, error) {
			return 0, nil
		},
	}
	svc := NewService(repo)

	removed, err := svc.RemoveWallets(context.Background(), owner, groupID, &WalletsRequest{
		Wallets: []string{uuid.New().String()},
	})
	if err != nil {
		t.Fatalf("expected idempotent success, got %v", err)
	}
	if removed != 0 {
		t.Errorf("expected 0 removed, got %d", removed)
	}
}

// ─── GRP-U-14: wallet in multiple groups ────────────────────────

func TestService_AddWallets_MultiGroup(t *testing.T) {
	owner := uuid.New()
	groupA, groupB := uuid.New(), uuid.New()
	addedTo := map[uuid.UUID]bool{}
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return &Group{ID: id, UserID: owner}, nil
		},
		addMembersFn: func(ctx context.Context, g, by uuid.UUID, items []walletItem) (int64, error) {
			addedTo[g] = true
			return 1, nil
		},
	}
	svc := NewService(repo)

	wallet := uuid.New().String()
	if _, err := svc.AddWallets(context.Background(), owner, groupA, &WalletsRequest{Wallets: []string{wallet}}); err != nil {
		t.Fatalf("add to group A failed: %v", err)
	}
	if _, err := svc.AddWallets(context.Background(), owner, groupB, &WalletsRequest{Wallets: []string{wallet}}); err != nil {
		t.Fatalf("add to group B failed: %v", err)
	}
	if !addedTo[groupA] || !addedTo[groupB] {
		t.Errorf("expected membership in both groups, got %v", addedTo)
	}
}

// ─── GRP-U-15: wallet_count returned with group ─────────────────

func TestService_GetGroup_ReturnsWalletCount(t *testing.T) {
	owner := uuid.New()
	groupID := uuid.New()
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return &Group{ID: groupID, UserID: owner, Name: "G", WalletCount: 7}, nil
		},
	}
	svc := NewService(repo)

	g, err := svc.GetGroup(context.Background(), owner, groupID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.WalletCount != 7 {
		t.Errorf("expected wallet_count 7, got %d", g.WalletCount)
	}
}

// ─── ownership: unknown → GROUP-001, foreign → GROUP-003 ────────

func TestService_GetGroup_UnknownID(t *testing.T) {
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return nil, pgx.ErrNoRows
		},
	}
	svc := NewService(repo)

	_, err := svc.GetGroup(context.Background(), uuid.New(), uuid.New())
	if code := appErrCode(t, err); code != domain.ErrCodeGroupNotFound {
		t.Errorf("expected GROUP-001, got %s", code)
	}
}

func TestService_GetGroup_ForeignGroup(t *testing.T) {
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return &Group{ID: id, UserID: uuid.New(), Name: "G"}, nil
		},
	}
	svc := NewService(repo)

	_, err := svc.GetGroup(context.Background(), uuid.New(), uuid.New())
	if code := appErrCode(t, err); code != domain.ErrCodeGroupForbidden {
		t.Errorf("expected GROUP-003, got %s", code)
	}
}

// ─── list: ownership + pagination meta ──────────────────────────

func TestService_ListGroupWallets_Meta(t *testing.T) {
	owner := uuid.New()
	groupID := uuid.New()
	repo := &mockRepo{
		getGroupByIDFn: func(ctx context.Context, id uuid.UUID) (*Group, error) {
			return &Group{ID: groupID, UserID: owner}, nil
		},
		listMembersFn: func(ctx context.Context, g uuid.UUID, search string, limit, offset int) ([]*WalletRef, int64, error) {
			if search != "0xabc" {
				t.Errorf("expected normalized search, got %q", search)
			}
			if limit != 5 || offset != 5 {
				t.Errorf("expected limit=5 offset=5 (page 2), got limit=%d offset=%d", limit, offset)
			}
			return []*WalletRef{}, 12, nil
		},
	}
	svc := NewService(repo)

	_, meta, err := svc.ListGroupWallets(context.Background(), owner, groupID, "  0xABC ", 2, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.Page != 2 || meta.Limit != 5 || meta.Offset != 5 {
		t.Errorf("unexpected meta page/limit/offset: %+v", meta)
	}
	if meta.TotalPages != 3 {
		t.Errorf("expected 3 total pages, got %d", meta.TotalPages)
	}
	if !meta.HasMore {
		t.Error("expected has_more true")
	}
}
