package auth

import (
	"context"
	"testing"

	"github.com/starlink/push/internal/domain"
	"github.com/starlink/push/internal/port"
)

// fakeAuthStore 只实现 syncBuiltinPermissions 用到的方法，其余返回零值。
type fakeAuthStore struct {
	port.AuthRepository

	perms     map[string]struct{}
	roles     []domain.AuthRole
	rolePerms map[string][]string
	replaced  map[string][]string
}

func newFakeAuthStore() *fakeAuthStore {
	return &fakeAuthStore{
		perms:     map[string]struct{}{},
		rolePerms: map[string][]string{},
		replaced:  map[string][]string{},
	}
}

func (f *fakeAuthStore) ListAllPermissionCodes(context.Context) ([]string, error) {
	out := make([]string, 0, len(f.perms))
	for c := range f.perms {
		out = append(out, c)
	}
	return out, nil
}

func (f *fakeAuthStore) CreatePermission(_ context.Context, p *domain.AuthPermission) error {
	f.perms[p.Code] = struct{}{}
	return nil
}

func (f *fakeAuthStore) ListRoles(context.Context) ([]domain.AuthRole, error) { return f.roles, nil }

func (f *fakeAuthStore) ListRolePermissions(_ context.Context, role string) ([]string, error) {
	return f.rolePerms[role], nil
}

func (f *fakeAuthStore) ReplaceRolePermissions(_ context.Context, role string, perms []string) error {
	f.replaced[role] = append([]string(nil), perms...)
	f.rolePerms[role] = append([]string(nil), perms...)
	return nil
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// 升级场景：库里已有旧版权限，新版新增的权限码必须补进去，
// 否则新功能上线后连 admin 都拿不到权限，只能人工插表。
func TestSyncBuiltinPermissionsBackfillsNewCodes(t *testing.T) {
	f := newFakeAuthStore()
	// 模拟旧版本：只有 menu.overview，且 admin 只被授予了它
	f.perms[PermMenuOverview] = struct{}{}
	f.roles = []domain.AuthRole{{Code: RoleAdmin, IsSystem: true}}
	f.rolePerms[RoleAdmin] = []string{PermMenuOverview}

	if err := syncBuiltinPermissions(context.Background(), f); err != nil {
		t.Fatalf("sync 失败: %v", err)
	}

	for _, code := range []string{PermMenuSegments, PermSegmentManage, PermPreferenceManage} {
		if _, ok := f.perms[code]; !ok {
			t.Errorf("新权限码 %s 未写入权限表", code)
		}
		if !contains(f.rolePerms[RoleAdmin], code) {
			t.Errorf("admin 未获得新权限 %s", code)
		}
	}
	// 原有授权不能丢
	if !contains(f.rolePerms[RoleAdmin], PermMenuOverview) {
		t.Error("同步不应丢掉角色原有权限")
	}
}

// 幂等：权限已齐全时不应重复写入，也不应改动任何角色绑定。
func TestSyncBuiltinPermissionsIsIdempotent(t *testing.T) {
	f := newFakeAuthStore()
	for _, meta := range BuiltinPermissionCatalog() {
		f.perms[meta.Code] = struct{}{}
	}
	f.roles = []domain.AuthRole{{Code: RoleAdmin, IsSystem: true}}
	f.rolePerms[RoleAdmin] = []string{PermMenuOverview}

	if err := syncBuiltinPermissions(context.Background(), f); err != nil {
		t.Fatalf("sync 失败: %v", err)
	}
	if len(f.replaced) != 0 {
		t.Fatalf("无新增权限时不应改动角色绑定，实际改了 %v", f.replaced)
	}
}

// 运营在后台自定义的角色不应被版本升级自动扩权。
func TestSyncBuiltinPermissionsSkipsCustomRoles(t *testing.T) {
	f := newFakeAuthStore()
	f.perms[PermMenuOverview] = struct{}{}
	f.roles = []domain.AuthRole{{Code: "custom-auditor"}}
	f.rolePerms["custom-auditor"] = []string{PermMenuOverview}

	if err := syncBuiltinPermissions(context.Background(), f); err != nil {
		t.Fatalf("sync 失败: %v", err)
	}
	if _, touched := f.replaced["custom-auditor"]; touched {
		t.Fatal("自定义角色不应被自动授予新权限")
	}
	// 权限码本身仍要补进目录，供管理员手动授予
	if _, ok := f.perms[PermSegmentManage]; !ok {
		t.Error("新权限码仍应写入权限表")
	}
}

// viewer 只该拿到内置定义里属于它的那部分菜单权限，不能顺带拿到写权限。
func TestSyncBuiltinPermissionsRespectsRoleDefinition(t *testing.T) {
	f := newFakeAuthStore()
	f.perms[PermMenuOverview] = struct{}{}
	f.roles = []domain.AuthRole{{Code: RoleViewer, IsSystem: true}}
	f.rolePerms[RoleViewer] = []string{PermMenuOverview}

	if err := syncBuiltinPermissions(context.Background(), f); err != nil {
		t.Fatalf("sync 失败: %v", err)
	}
	if !contains(f.rolePerms[RoleViewer], PermMenuSegments) {
		t.Error("viewer 应获得人群资产菜单权限")
	}
	if contains(f.rolePerms[RoleViewer], PermSegmentManage) {
		t.Error("viewer 不应获得人群段写权限")
	}
	if contains(f.rolePerms[RoleViewer], PermPreferenceView) {
		t.Error("viewer 不应获得用户偏好读权限")
	}
}

func TestDedupStrings(t *testing.T) {
	got := dedupStrings([]string{"a", "", "b", "a", "b", "c"})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("去重结果 = %v，期望 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("去重应保持首次出现顺序: %v", got)
		}
	}
}
