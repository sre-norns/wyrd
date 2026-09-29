package identity

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	e "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/internal/pgtest"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
)

type pagingFixture struct {
	db      *gorm.DB
	s       *Service
	userID  string
	account e.AccountID
	owner   context.Context
}

// newPagingOwner provisions a user owning a fresh account, and the context
// acting as that user in it.
func newPagingOwner(t *testing.T, db *gorm.DB, s *Service, email, account string) pagingFixture {
	t.Helper()
	ctx := context.Background()
	check(t, s.ProvisionUser(ctx, email, "correct horse battery", false))
	var u user
	check(t, db.Where("email = ?", email).First(&u).Error)
	a, _, err := s.Accounts().CreateOrUpdate(WithPrincipal(ctx, e.Principal{Type: "user", UserID: u.ID}), e.Account{Resource: e.Resource{Name: account}})
	check(t, err)
	owner := WithPrincipal(ctx, e.Principal{Type: "user", UserID: u.ID, AccountID: e.AccountID(a.ID), Scope: e.ScopeAccount})
	return pagingFixture{db: db, s: s, userID: u.ID, account: e.AccountID(a.ID), owner: owner}
}

func newPagingService(t *testing.T) (*gorm.DB, *Service) {
	t.Helper()
	db := pgtest.Open(t)
	check(t, Register(db, Extensions{}))
	check(t, Migrate(db))
	return db, NewService(db)
}

// pageThrough follows next until the last page, returning the IDs seen.
func pageThrough[T any](t *testing.T, limit uint, list func(manifest.SearchQuery) ([]T, manifest.Page, error), id func(T) string) []string {
	t.Helper()
	var ids []string
	q := manifest.SearchQuery{Limit: limit}
	for n := 0; ; n++ {
		items, page, err := list(q)
		check(t, err)
		if uint(len(items)) > limit {
			t.Fatalf("page of %d exceeds limit %d", len(items), limit)
		}
		for _, item := range items {
			ids = append(ids, id(item))
		}
		if page.Next == "" {
			return ids
		}
		if uint(len(items)) != limit {
			t.Fatalf("a short page (%d of %d) has a next page", len(items), limit)
		}
		q.Cursor = page.Next
		if n > 50 {
			t.Fatal("paging did not terminate")
		}
	}
}

func requireProblem(t *testing.T, err error, code string) {
	t.Helper()
	var p *Problem
	if !errors.As(err, &p) || p.Code != code || p.Status != 400 {
		t.Fatalf("want a 400 %q problem, got %v", code, err)
	}
}

func TestListsPageNewestFirstAndBreakTiesByID(t *testing.T) {
	db, s := newPagingService(t)
	a := newPagingOwner(t, db, s, "owner@example.test", "acme")

	var want []e.Project
	for i := range 5 {
		p, err := s.Projects().Create(a.owner, e.Project{Resource: e.Resource{Name: fmt.Sprintf("p%d", i), AccountID: a.account}})
		check(t, err)
		want = append(want, p)
	}
	// Three projects share a creation time, so only the ID can order them.
	tied := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	check(t, db.Model(&e.Project{}).Where("id IN ?", []string{want[1].ID, want[2].ID, want[3].ID}).Update("created_at", tied).Error)
	check(t, db.Model(&e.Project{}).Where("id = ?", want[0].ID).Update("created_at", tied.Add(-time.Hour)).Error)
	check(t, db.Model(&e.Project{}).Where("id = ?", want[4].ID).Update("created_at", tied.Add(time.Hour)).Error)

	tiedIDs := []string{want[1].ID, want[2].ID, want[3].ID}
	slices.Sort(tiedIDs)
	slices.Reverse(tiedIDs)
	expected := append(append([]string{want[4].ID}, tiedIDs...), want[0].ID)

	for _, limit := range []uint{1, 2, 3, 5, 100} {
		got := pageThrough(t, limit, func(q manifest.SearchQuery) ([]e.Project, manifest.Page, error) {
			return s.Projects().List(a.owner, q)
		}, func(p e.Project) string { return p.ID })
		if !slices.Equal(got, expected) {
			t.Fatalf("limit %d: got %v, want %v", limit, got, expected)
		}
	}

	_, page, err := s.Projects().List(a.owner, manifest.SearchQuery{Limit: 2})
	check(t, err)
	if page.Total == nil || *page.Total != 5 || page.Limit != 2 {
		t.Fatalf("page = %+v", page)
	}

	// Filters apply before paging: the last page of a filter has no next,
	// though unfiltered rows follow it.
	sel, err := manifest.ParseSelector("")
	check(t, err)
	items, page, err := s.Projects().List(a.owner, manifest.SearchQuery{Selector: sel, Name: "p4", Limit: 1})
	check(t, err)
	if len(items) != 1 || items[0].ID != want[4].ID || page.Next != "" || *page.Total != 1 {
		t.Fatalf("filtered page: %v %+v", items, page)
	}
}

func TestListCursorsNeverWidenVisibility(t *testing.T) {
	db, s := newPagingService(t)
	a := newPagingOwner(t, db, s, "a@example.test", "acme")
	b := newPagingOwner(t, db, s, "b@example.test", "globex")
	for i := range 4 {
		_, err := s.Projects().Create(a.owner, e.Project{Resource: e.Resource{Name: fmt.Sprintf("a%d", i), AccountID: a.account}})
		check(t, err)
		_, err = s.Projects().Create(b.owner, e.Project{Resource: e.Resource{Name: fmt.Sprintf("b%d", i), AccountID: b.account}})
		check(t, err)
	}

	_, page, err := s.Projects().List(a.owner, manifest.SearchQuery{Limit: 1})
	check(t, err)
	// B presents A's cursor: the position is honoured, but only B's rows are.
	items, _, err := s.Projects().List(b.owner, manifest.SearchQuery{Cursor: page.Next})
	check(t, err)
	for _, p := range items {
		if p.AccountID != b.account {
			t.Fatalf("a cursor from account A disclosed %s of account %s", p.Name, p.AccountID)
		}
	}
	ids := pageThrough(t, 1, func(q manifest.SearchQuery) ([]e.Project, manifest.Page, error) {
		return s.Projects().List(b.owner, q)
	}, func(p e.Project) string { return string(p.AccountID) })
	if len(ids) != 4 || slices.ContainsFunc(ids, func(id string) bool { return id != string(b.account) }) {
		t.Fatalf("B paged through %v", ids)
	}

	// Account memberships stay the account admin's, whatever the cursor.
	_, _, err = s.AccountMemberships().List(b.owner, a.account, manifest.SearchQuery{Cursor: page.Next})
	if err == nil {
		t.Fatal("another account's members were listed")
	}
}

func TestListRefusesForeignCursorsAndOffsets(t *testing.T) {
	db, s := newPagingService(t)
	a := newPagingOwner(t, db, s, "owner@example.test", "acme")
	for i := range 3 {
		_, err := s.Projects().Create(a.owner, e.Project{Resource: e.Resource{Name: fmt.Sprintf("p%d", i), AccountID: a.account}})
		check(t, err)
		_, err = s.AccountInvitations().Create(a.owner, a.account, e.AccountInvitation{Email: fmt.Sprintf("x%d@example.test", i), Role: "member"})
		check(t, err)
	}

	_, _, err := s.Projects().List(a.owner, manifest.SearchQuery{Cursor: "not-a-cursor"})
	requireProblem(t, err, "invalid-cursor")
	_, _, err = s.Projects().List(a.owner, manifest.SearchQuery{Offset: 2})
	requireProblem(t, err, "offset-unsupported")

	byEmail := WithRequest(a.owner, Request{Sort: "email"})
	_, page, err := s.AccountInvitations().List(byEmail, a.account, manifest.SearchQuery{Limit: 1})
	check(t, err)
	_, _, err = s.AccountInvitations().List(WithRequest(a.owner, Request{Sort: "role"}), a.account, manifest.SearchQuery{Cursor: page.Next})
	requireProblem(t, err, "invalid-cursor")
	_, _, err = s.AccountInvitations().List(WithRequest(a.owner, Request{Sort: "email", Direction: "desc"}), a.account, manifest.SearchQuery{Cursor: page.Next})
	requireProblem(t, err, "invalid-cursor")
}

func TestInvitationSortsPageInTheirOwnOrder(t *testing.T) {
	db, s := newPagingService(t)
	a := newPagingOwner(t, db, s, "owner@example.test", "acme")
	emails := []string{"delta@example.test", "alpha@example.test", "charlie@example.test", "bravo@example.test", "echo@example.test"}
	byEmail := map[string]string{}
	for _, email := range emails {
		inv, err := s.AccountInvitations().Create(a.owner, a.account, e.AccountInvitation{Email: email, Role: "member"})
		check(t, err)
		byEmail[inv.ID] = email
	}
	// Two have expired: a status sort sees them as "expired", which exists
	// only in SQL, not in the stored column.
	check(t, db.Model(&e.AccountInvitation{}).Where("email IN ?", []string{"bravo@example.test", "echo@example.test"}).Update("expires_at", time.Now().Add(-time.Hour)).Error)

	list := func(sort, direction string) []string {
		ctx := WithRequest(a.owner, Request{Sort: sort, Direction: direction})
		ids := pageThrough(t, 2, func(q manifest.SearchQuery) ([]e.AccountInvitation, manifest.Page, error) {
			return s.AccountInvitations().List(ctx, a.account, q)
		}, func(i e.AccountInvitation) string { return i.ID })
		out := make([]string, len(ids))
		for i, id := range ids {
			out[i] = byEmail[id]
		}
		return out
	}

	sorted := slices.Clone(emails)
	slices.Sort(sorted)
	if got := list("email", ""); !slices.Equal(got, sorted) {
		t.Fatalf("email asc: %v", got)
	}
	slices.Reverse(sorted)
	if got := list("email", "desc"); !slices.Equal(got, sorted) {
		t.Fatalf("email desc: %v", got)
	}

	got := list("status", "asc")
	if len(got) != 5 || !slices.Contains(got[:2], "bravo@example.test") || !slices.Contains(got[:2], "echo@example.test") {
		t.Fatalf("status asc should lead with the two expired: %v", got)
	}
	if got := list("email_status", ""); len(got) != 5 {
		t.Fatalf("email_status: %v", got)
	}
	if got := list("last_attempt_at", "desc"); len(got) != 5 {
		t.Fatalf("last_attempt_at: %v", got)
	}
	if got := list("", ""); len(got) != 5 || got[0] != "echo@example.test" {
		t.Fatalf("default is newest first: %v", got)
	}
}

func TestProfileAccountsPageByName(t *testing.T) {
	db, s := newPagingService(t)
	a := newPagingOwner(t, db, s, "owner@example.test", "zulu")
	for _, name := range []string{"alpha", "mike", "alpha-2"} {
		_, _, err := s.Accounts().CreateOrUpdate(WithPrincipal(context.Background(), e.Principal{Type: "user", UserID: a.userID}), e.Account{Resource: e.Resource{Name: name}})
		check(t, err)
	}
	var names []string
	var total int64
	q := manifest.SearchQuery{Limit: 1}
	for {
		accounts, page, err := s.PersonalProfile().AccessibleAccounts(a.owner, q)
		check(t, err)
		if page.Total == nil {
			t.Fatal("no total")
		}
		total = *page.Total
		for _, acc := range accounts {
			names = append(names, acc.Name)
		}
		if page.Next == "" {
			break
		}
		q.Cursor = page.Next
	}
	// Provisioning may give the user an account of its own besides these.
	if int64(len(names)) != total || !slices.IsSorted(names) || len(names) < 4 {
		t.Fatalf("accounts: %v of %d", names, total)
	}
	for _, name := range []string{"alpha", "alpha-2", "mike", "zulu"} {
		if !slices.Contains(names, name) {
			t.Fatalf("%s missing from %v", name, names)
		}
	}
}
