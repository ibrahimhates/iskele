package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ibrahimhates/iskele/internal/auth"
	"github.com/ibrahimhates/iskele/internal/docker/fake"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/store"
)

// sendJSON marshals body and issues an authenticated request. A nil body sends
// an empty JSON object, which is what the endpoints taking no arguments want.
func sendJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	encoded := []byte("{}")
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
	}

	req := httptest.NewRequest(method, path, strings.NewReader(string(encoded)))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// bodyOf decodes a JSON object response.
func bodyOf(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not a JSON object: %v (%q)", err, rec.Body.String())
	}
	return out
}

// userID looks up a fixture account's identifier.
func (e *testEnv) userID(t *testing.T, username string) string {
	t.Helper()

	user, err := e.db.Users.ByUsername(t.Context(), username)
	if err != nil {
		t.Fatalf("Users.ByUsername(%s) error = %v", username, err)
	}
	return user.ID
}

// enrollTOTP takes an account all the way through enrollment and returns the
// secret, so the test can produce codes for it.
func (e *testEnv) enrollTOTP(t *testing.T, h http.Handler) string {
	t.Helper()

	setup := sendJSON(t, h, http.MethodPost, APIPrefix+"/auth/totp/setup", nil)
	if setup.Code != http.StatusCreated {
		t.Fatalf("totp setup = %d: %s", setup.Code, setup.Body)
	}
	secret, _ := bodyOf(t, setup)["secret"].(string)

	code, err := auth.TOTPCode(secret, time.Now())
	if err != nil {
		t.Fatalf("TOTPCode() error = %v", err)
	}
	confirm := sendJSON(t, h, http.MethodPost, APIPrefix+"/auth/totp/verify",
		map[string]any{"code": code})
	if confirm.Code != http.StatusOK {
		t.Fatalf("totp verify = %d: %s", confirm.Code, confirm.Body)
	}
	return secret
}

func TestUsersListShowsEveryAccountWithoutSecrets(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := request(t, env.as(store.RoleAdmin), http.MethodGet, APIPrefix+"/users")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	items, total := listOf(t, rec)
	if total != 3 {
		t.Fatalf("total = %d, want the three fixture accounts", total)
	}

	// The password hash and the two-factor secret have no business leaving the
	// server, and a struct tag is the only thing keeping them in.
	body := rec.Body.String()
	for _, leaked := range []string{"password_hash", "totp_secret", "$argon2id$"} {
		if strings.Contains(body, leaked) {
			t.Errorf("the listing contains %q", leaked)
		}
	}
	for _, item := range items {
		if _, present := item["totp_enabled"]; !present {
			t.Errorf("account %v does not report its two-factor state", item["username"])
		}
		// The listing goes out through the same view the session endpoints
		// use, so it carries the role's permissions like every other account
		// response.
		if perms, _ := item["permissions"].([]any); len(perms) == 0 {
			t.Errorf("account %v lists no permissions", item["username"])
		}
	}
}

func TestUsersCreateAndSignIn(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := sendJSON(t, env.as(store.RoleAdmin), http.MethodPost, APIPrefix+"/users", map[string]any{
		"username": "deploy",
		"password": testPassword,
		"role":     "operator",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}
	if role := bodyOf(t, rec)["role"]; role != "operator" {
		t.Errorf("role = %v", role)
	}

	// The account is real: it signs in, and the token it gets works.
	token := env.login(t, "deploy", testPassword)
	me := request(t, env.withToken(token), http.MethodGet, APIPrefix+"/auth/me")
	if me.Code != http.StatusOK {
		t.Fatalf("the new account cannot use its token: %d", me.Code)
	}
}

func TestUsersCreateRefusesABadRequest(t *testing.T) {
	env := newEnv(t, fake.New())
	h := env.as(store.RoleAdmin)

	cases := map[string]map[string]any{
		"no username":    {"username": "  ", "password": testPassword, "role": "viewer"},
		"weak password":  {"username": "weak", "password": "short", "role": "viewer"},
		"unknown role":   {"username": "odd", "password": testPassword, "role": "superuser"},
		"missing role":   {"username": "norole", "password": testPassword},
		"taken username": {"username": "admin", "password": testPassword, "role": "viewer"},
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := sendJSON(t, h, http.MethodPost, APIPrefix+"/users", body)
			if rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 422 or 409: %s", rec.Code, rec.Body)
			}
		})
	}
}

// An update carries only the fields that changed, so one form can set a role
// without also resetting a password.
func TestUsersUpdateChangesOnlyWhatWasSent(t *testing.T) {
	env := newEnv(t, fake.New())
	id := env.userID(t, "viewer")

	rec := sendJSON(t, env.as(store.RoleAdmin), http.MethodPut, APIPrefix+"/users/"+id,
		map[string]any{"role": "operator"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if role := bodyOf(t, rec)["role"]; role != "operator" {
		t.Errorf("role = %v", role)
	}

	// The password was not in the body, so it must still work.
	env.login(t, "viewer", testPassword)
}

// Resetting a password hands an account to somebody, or takes it away; either
// way the sessions opened before now must stop working.
func TestUsersPasswordResetEndsTheAccountsSessions(t *testing.T) {
	env := newEnv(t, fake.New())

	login := sendJSON(t, env.anonymous(), http.MethodPost, APIPrefix+"/auth/login",
		map[string]any{"username": "viewer", "password": testPassword})
	if login.Code != http.StatusOK {
		t.Fatalf("the fixture account cannot sign in: %d", login.Code)
	}
	refresh, _ := bodyOf(t, login)["refresh_token"].(string)

	newPassword := "another-correct-Horse-2"
	rec := sendJSON(t, env.as(store.RoleAdmin), http.MethodPut,
		APIPrefix+"/users/"+env.userID(t, "viewer"), map[string]any{"password": newPassword})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	renew := sendJSON(t, env.anonymous(), http.MethodPost, APIPrefix+"/auth/refresh",
		map[string]any{"refresh_token": refresh})
	if renew.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after a password reset = %d, want 401", renew.Code)
	}

	// And the new password is the one that works.
	env.login(t, "viewer", newPassword)
}

func TestUsersDisableEndsTheSessionAndRefusesTheLogin(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := sendJSON(t, env.as(store.RoleAdmin), http.MethodPut,
		APIPrefix+"/users/"+env.userID(t, "operator"), map[string]any{"disabled": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	// The access token it already held is refused, because the account is.
	if me := request(t, env.as(store.RoleOperator), http.MethodGet,
		APIPrefix+"/auth/me"); me.Code == http.StatusOK {
		t.Error("a disabled account's token still works")
	}

	login := sendJSON(t, env.anonymous(), http.MethodPost, APIPrefix+"/auth/login",
		map[string]any{"username": "operator", "password": testPassword})
	if login.Code != http.StatusForbidden {
		t.Fatalf("login of a disabled account = %d, want 403", login.Code)
	}
	if code, _ := errorOf(t, login); code != string(httpx.CodeAccountDisabled) {
		t.Errorf("code = %q, want %q", code, httpx.CodeAccountDisabled)
	}
}

// The one mistake that locks everybody out of the panel. The guard covers the
// caller's own account too, which is the only way it can ever fire: whoever
// makes the change is an admin, so a second admin always exists unless the
// target is the caller.
func TestUsersRefuseToLeaveThePanelWithoutAnAdmin(t *testing.T) {
	env := newEnv(t, fake.New())
	h := env.as(store.RoleAdmin)
	own := env.userID(t, "admin")

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"demote":  sendJSON(t, h, http.MethodPut, APIPrefix+"/users/"+own, map[string]any{"role": "viewer"}),
		"disable": sendJSON(t, h, http.MethodPut, APIPrefix+"/users/"+own, map[string]any{"disabled": true}),
	} {
		if rec.Code != http.StatusConflict {
			t.Errorf("%s of the last admin = %d, want 409: %s", name, rec.Code, rec.Body)
			continue
		}
		if code, _ := errorOf(t, rec); code != string(httpx.CodeLastAdmin) {
			t.Errorf("%s: code = %q, want %q", name, code, httpx.CodeLastAdmin)
		}
	}

	// The account is untouched, so the panel still has its admin.
	if role := bodyOf(t, request(t, h, http.MethodGet, APIPrefix+"/users/"+own))["role"]; role != "admin" {
		t.Fatalf("role = %v after the refused changes", role)
	}
}

// Stepping down is a legitimate thing to do — once somebody else can take
// over. The guard is about the invariant, not about who asked.
func TestUsersAllowSteppingDownOnceAnotherAdminExists(t *testing.T) {
	env := newEnv(t, fake.New())
	h := env.as(store.RoleAdmin)

	created := sendJSON(t, h, http.MethodPost, APIPrefix+"/users", map[string]any{
		"username": "admin2", "password": testPassword, "role": "admin",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("could not create a second admin: %s", created.Body)
	}

	own := env.userID(t, "admin")
	rec := sendJSON(t, h, http.MethodPut, APIPrefix+"/users/"+own, map[string]any{"role": "operator"})
	if rec.Code != http.StatusOK {
		t.Fatalf("stepping down = %d, want 200: %s", rec.Code, rec.Body)
	}

	// And the demotion took effect: the account can no longer administer.
	if after := request(t, h, http.MethodGet, APIPrefix+"/users"); after.Code != http.StatusForbidden {
		t.Fatalf("the demoted account still reaches the account list: %d", after.Code)
	}
}

// A disabled admin does not count towards the invariant: it cannot sign in, so
// it cannot administer anything.
func TestUsersDoNotCountADisabledAdmin(t *testing.T) {
	env := newEnv(t, fake.New())
	h := env.as(store.RoleAdmin)

	created := sendJSON(t, h, http.MethodPost, APIPrefix+"/users", map[string]any{
		"username": "admin2", "password": testPassword, "role": "admin",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("could not create a second admin: %s", created.Body)
	}
	second, _ := bodyOf(t, created)["id"].(string)

	if rec := sendJSON(t, h, http.MethodPut, APIPrefix+"/users/"+second,
		map[string]any{"disabled": true}); rec.Code != http.StatusOK {
		t.Fatalf("disabling the second admin = %d: %s", rec.Code, rec.Body)
	}

	// The caller is now the only admin who can actually sign in.
	rec := sendJSON(t, h, http.MethodPut, APIPrefix+"/users/"+env.userID(t, "admin"),
		map[string]any{"role": "viewer"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("stepping down behind a disabled admin = %d, want 409: %s", rec.Code, rec.Body)
	}
}

func TestUsersRefuseDeletingTheCallersOwnAccount(t *testing.T) {
	env := newEnv(t, fake.New())
	h := env.as(store.RoleAdmin)

	// Even with a second admin available to clean up afterwards: deleting the
	// account the request came from is nobody's intention.
	if created := sendJSON(t, h, http.MethodPost, APIPrefix+"/users", map[string]any{
		"username": "admin2", "password": testPassword, "role": "admin",
	}); created.Code != http.StatusCreated {
		t.Fatalf("could not create a second admin: %s", created.Body)
	}

	rec := sendJSON(t, h, http.MethodDelete, APIPrefix+"/users/"+env.userID(t, "admin"), nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("deleting one's own account = %d, want 403: %s", rec.Code, rec.Body)
	}

	// Changing one's own password is a different thing, and stays allowed.
	own := sendJSON(t, h, http.MethodPut, APIPrefix+"/users/"+env.userID(t, "admin"),
		map[string]any{"password": "yet-another-Horse-3"})
	if own.Code != http.StatusOK {
		t.Fatalf("changing one's own password = %d: %s", own.Code, own.Body)
	}
}

func TestUsersDeleteRemovesTheAccount(t *testing.T) {
	env := newEnv(t, fake.New())
	h := env.as(store.RoleAdmin)
	id := env.userID(t, "viewer")

	rec := sendJSON(t, h, http.MethodDelete, APIPrefix+"/users/"+id, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body)
	}

	if gone := request(t, h, http.MethodGet, APIPrefix+"/users/"+id); gone.Code != http.StatusNotFound {
		t.Fatalf("the deleted account is still there: %d", gone.Code)
	}

	login := sendJSON(t, env.anonymous(), http.MethodPost, APIPrefix+"/auth/login",
		map[string]any{"username": "viewer", "password": testPassword})
	if login.Code != http.StatusUnauthorized {
		t.Fatalf("a deleted account signed in: %d", login.Code)
	}
}

func TestUsersAreAdminOnly(t *testing.T) {
	env := newEnv(t, fake.New())

	for _, role := range []store.Role{store.RoleOperator, store.RoleViewer} {
		if rec := request(t, env.as(role), http.MethodGet, APIPrefix+"/users"); rec.Code != http.StatusForbidden {
			t.Errorf("%s reached the account list: %d", role, rec.Code)
		}
	}
}

// The whole two-factor round trip: enroll, confirm with a real code, sign in
// with one, and be refused without one.
func TestTOTPEnrollmentAndLogin(t *testing.T) {
	env := newEnv(t, fake.New())
	h := env.as(store.RoleAdmin)

	setup := sendJSON(t, h, http.MethodPost, APIPrefix+"/auth/totp/setup", nil)
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup = %d, want 201: %s", setup.Code, setup.Body)
	}
	enrollment := bodyOf(t, setup)
	secret, _ := enrollment["secret"].(string)
	uri, _ := enrollment["uri"].(string)
	if secret == "" || !strings.HasPrefix(uri, "otpauth://totp/") {
		t.Fatalf("enrollment = %+v", enrollment)
	}

	// Until it is confirmed, the account still signs in with a password alone:
	// an abandoned enrollment must not lock anybody out.
	if login := sendJSON(t, env.anonymous(), http.MethodPost, APIPrefix+"/auth/login",
		map[string]any{"username": "admin", "password": testPassword}); login.Code != http.StatusOK {
		t.Fatalf("login during an unconfirmed enrollment = %d", login.Code)
	}

	// A wrong code does not enable it either.
	if bad := sendJSON(t, h, http.MethodPost, APIPrefix+"/auth/totp/verify",
		map[string]any{"code": "000000"}); bad.Code != http.StatusUnauthorized {
		t.Fatalf("verify with a wrong code = %d, want 401", bad.Code)
	}

	code, err := auth.TOTPCode(secret, time.Now())
	if err != nil {
		t.Fatalf("TOTPCode() error = %v", err)
	}
	if ok := sendJSON(t, h, http.MethodPost, APIPrefix+"/auth/totp/verify",
		map[string]any{"code": code}); ok.Code != http.StatusOK {
		t.Fatalf("verify = %d: %s", ok.Code, ok.Body)
	}

	// Now a password alone is not enough, and the form is told why.
	noCode := sendJSON(t, env.anonymous(), http.MethodPost, APIPrefix+"/auth/login",
		map[string]any{"username": "admin", "password": testPassword})
	if noCode.Code != http.StatusUnauthorized {
		t.Fatalf("login without a code = %d, want 401", noCode.Code)
	}
	if errCode, _ := errorOf(t, noCode); errCode != string(httpx.CodeTOTPRequired) {
		t.Errorf("code = %q, want %q", errCode, httpx.CodeTOTPRequired)
	}

	// A wrong code is an invalid login, not a distinguishable "wrong code".
	wrong := sendJSON(t, env.anonymous(), http.MethodPost, APIPrefix+"/auth/login",
		map[string]any{"username": "admin", "password": testPassword, "totp": "000000"})
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("login with a wrong code = %d, want 401", wrong.Code)
	}
	if errCode, _ := errorOf(t, wrong); errCode != string(httpx.CodeInvalidCredentials) {
		t.Errorf("code = %q, want %q", errCode, httpx.CodeInvalidCredentials)
	}

	fresh, _ := auth.TOTPCode(secret, time.Now())
	good := sendJSON(t, env.anonymous(), http.MethodPost, APIPrefix+"/auth/login",
		map[string]any{"username": "admin", "password": testPassword, "totp": fresh})
	if good.Code != http.StatusOK {
		t.Fatalf("login with a code = %d: %s", good.Code, good.Body)
	}
}

// Turning two-factor off asks for a code: an unattended browser must not be
// enough to remove the factor that guards against unattended browsers.
func TestTOTPDisableRequiresACode(t *testing.T) {
	env := newEnv(t, fake.New())
	h := env.as(store.RoleAdmin)
	secret := env.enrollTOTP(t, h)

	if bad := sendJSON(t, h, http.MethodPost, APIPrefix+"/auth/totp/disable",
		map[string]any{"code": "000000"}); bad.Code != http.StatusUnauthorized {
		t.Fatalf("disable with a wrong code = %d, want 401", bad.Code)
	}

	code, _ := auth.TOTPCode(secret, time.Now())
	if ok := sendJSON(t, h, http.MethodPost, APIPrefix+"/auth/totp/disable",
		map[string]any{"code": code}); ok.Code != http.StatusOK {
		t.Fatalf("disable = %d: %s", ok.Code, ok.Body)
	}

	// The password alone is enough again.
	if login := sendJSON(t, env.anonymous(), http.MethodPost, APIPrefix+"/auth/login",
		map[string]any{"username": "admin", "password": testPassword}); login.Code != http.StatusOK {
		t.Fatalf("login after disabling = %d", login.Code)
	}
}

// The locked-out case: a lost phone, and an admin who has to let its owner
// back in.
func TestTOTPResetClearsAnotherAccountsFactor(t *testing.T) {
	env := newEnv(t, fake.New())
	env.enrollTOTP(t, env.as(store.RoleOperator))

	admin := env.as(store.RoleAdmin)
	if own := sendJSON(t, admin, http.MethodDelete,
		APIPrefix+"/users/"+env.userID(t, "admin")+"/totp", nil); own.Code != http.StatusForbidden {
		t.Fatalf("resetting one's own factor = %d, want 403", own.Code)
	}

	rec := sendJSON(t, admin, http.MethodDelete,
		APIPrefix+"/users/"+env.userID(t, "operator")+"/totp", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("reset = %d, want 204: %s", rec.Code, rec.Body)
	}

	if login := sendJSON(t, env.anonymous(), http.MethodPost, APIPrefix+"/auth/login",
		map[string]any{"username": "operator", "password": testPassword}); login.Code != http.StatusOK {
		t.Fatalf("login after a reset = %d", login.Code)
	}
}

func TestTOTPEnrollmentNeedsNoPermissionButResetDoes(t *testing.T) {
	env := newEnv(t, fake.New())
	viewer := env.as(store.RoleViewer)

	// A viewer has no admin permission, yet enrolling is still their own
	// business: the endpoint acts on the caller and nobody else.
	if setup := sendJSON(t, viewer, http.MethodPost,
		APIPrefix+"/auth/totp/setup", nil); setup.Code != http.StatusCreated {
		t.Fatalf("a viewer cannot enroll: %d", setup.Code)
	}

	// Clearing somebody else's factor is admin-only.
	if rec := sendJSON(t, viewer, http.MethodDelete,
		APIPrefix+"/users/"+env.userID(t, "admin")+"/totp", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("a viewer reset another account's factor: %d", rec.Code)
	}
}

func TestTOTPSetupRefusesASecondEnrollment(t *testing.T) {
	env := newEnv(t, fake.New())
	h := env.as(store.RoleAdmin)
	env.enrollTOTP(t, h)

	if again := sendJSON(t, h, http.MethodPost,
		APIPrefix+"/auth/totp/setup", nil); again.Code != http.StatusConflict {
		t.Fatalf("a second enrollment = %d, want 409", again.Code)
	}
}
