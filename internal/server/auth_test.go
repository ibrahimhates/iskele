package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibrahimhates/iskele/internal/config"
	"github.com/ibrahimhates/iskele/internal/docker/fake"
	"github.com/ibrahimhates/iskele/internal/httpx"
	"github.com/ibrahimhates/iskele/internal/store"
)

// postJSON issues a request with a JSON body, optionally authenticated.
func postJSON(t *testing.T, h http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// newBareServer builds a router whose store has no accounts yet.
func newBareServer(t *testing.T) http.Handler {
	t.Helper()

	db, err := store.Open(context.Background(), store.Options{
		Path: filepath.Join(t.TempDir(), "test.db"),
	})
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.Default()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	return NewRouter(Deps{
		Config: &cfg,
		Logger: log,
		Docker: fake.New(),
		Auth:   newAuthService(db, log),
	})
}

func decodeSession(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v (%q)", err, rec.Body.String())
	}
	return body
}

func TestBootstrapCreatesTheFirstAdmin(t *testing.T) {
	h := newBareServer(t)

	rec := postJSON(t, h, http.MethodPost, APIPrefix+"/auth/bootstrap",
		`{"username":"alice","password":"`+testPassword+`"}`, "")

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	body := decodeSession(t, rec)

	if body["access_token"] == "" || body["refresh_token"] == "" {
		t.Error("bootstrap did not return a token pair")
	}
	user, ok := body["user"].(map[string]any)
	if !ok {
		t.Fatalf("user = %v", body["user"])
	}
	if user["role"] != string(store.RoleAdmin) {
		t.Errorf("role = %v, want the first account to be an admin", user["role"])
	}
	// The response must never carry credential material.
	if strings.Contains(rec.Body.String(), "password_hash") || strings.Contains(rec.Body.String(), "argon2") {
		t.Error("the response leaked the password hash")
	}
}

func TestBootstrapOnlyWorksOnce(t *testing.T) {
	h := newBareServer(t)
	body := `{"username":"alice","password":"` + testPassword + `"}`

	if rec := postJSON(t, h, http.MethodPost, APIPrefix+"/auth/bootstrap", body, ""); rec.Code != http.StatusCreated {
		t.Fatalf("first bootstrap failed: %d %s", rec.Code, rec.Body.String())
	}

	// A second call must not let a passer-by claim the installation.
	rec := postJSON(t, h, http.MethodPost, APIPrefix+"/auth/bootstrap",
		`{"username":"mallory","password":"`+testPassword+`"}`, "")

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if code, _ := errorOf(t, rec); code != string(httpx.CodeAlreadyInitialized) {
		t.Errorf("code = %q, want %q", code, httpx.CodeAlreadyInitialized)
	}
}

func TestUninitializedInstallationClosesTheAPI(t *testing.T) {
	h := newBareServer(t)

	// Before bootstrap, Docker must not be reachable through the API at all.
	rec := request(t, h, http.MethodGet, APIPrefix+"/containers")

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	code, msg := errorOf(t, rec)
	if code != string(httpx.CodeNotInitialized) {
		t.Errorf("code = %q, want %q", code, httpx.CodeNotInitialized)
	}
	if !strings.Contains(msg, "bootstrap") {
		t.Errorf("message = %q, want it to name the next step", msg)
	}
}

func TestAuthStatusReportsInitialization(t *testing.T) {
	h := newBareServer(t)

	rec := request(t, h, http.MethodGet, APIPrefix+"/auth/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := decodeSession(t, rec); body["initialized"] != false {
		t.Errorf("initialized = %v, want false", body["initialized"])
	}

	postJSON(t, h, http.MethodPost, APIPrefix+"/auth/bootstrap",
		`{"username":"alice","password":"`+testPassword+`"}`, "")

	rec = request(t, h, http.MethodGet, APIPrefix+"/auth/status")
	if body := decodeSession(t, rec); body["initialized"] != true {
		t.Errorf("initialized = %v, want true after bootstrap", body["initialized"])
	}
}

func TestBootstrapEnforcesThePasswordPolicy(t *testing.T) {
	tests := map[string]string{
		"too short":    "Short1!",
		"single class": "aaaaaaaaaaaaaaaaaaaa",
		"empty":        "",
	}

	for name, password := range tests {
		t.Run(name, func(t *testing.T) {
			h := newBareServer(t)

			rec := postJSON(t, h, http.MethodPost, APIPrefix+"/auth/bootstrap",
				`{"username":"alice","password":"`+password+`"}`, "")

			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body.String())
			}
			if code, _ := errorOf(t, rec); code != string(httpx.CodeValidationFailed) {
				t.Errorf("code = %q", code)
			}
		})
	}
}

func TestBootstrapRequiresAUsername(t *testing.T) {
	h := newBareServer(t)

	rec := postJSON(t, h, http.MethodPost, APIPrefix+"/auth/bootstrap",
		`{"username":"   ","password":"`+testPassword+`"}`, "")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestLoginAndUseTheAccessToken(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := postJSON(t, env.raw, http.MethodPost, APIPrefix+"/auth/login",
		`{"username":"admin","password":"`+testPassword+`"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	body := decodeSession(t, rec)
	token, _ := body["access_token"].(string)
	if token == "" {
		t.Fatal("login returned no access token")
	}
	if body["token_type"] != "Bearer" {
		t.Errorf("token_type = %v", body["token_type"])
	}

	if rec := request(t, env.withToken(token), http.MethodGet, APIPrefix+"/auth/me"); rec.Code != http.StatusOK {
		t.Fatalf("the freshly issued token was rejected: %d", rec.Code)
	}
}

func TestLoginIsCaseInsensitiveOnUsername(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := postJSON(t, env.raw, http.MethodPost, APIPrefix+"/auth/login",
		`{"username":"ADMIN","password":"`+testPassword+`"}`, "")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want the username lookup to ignore case", rec.Code)
	}
}

func TestLoginFailuresAreIndistinguishable(t *testing.T) {
	env := newEnv(t, fake.New())

	wrongPassword := postJSON(t, env.raw, http.MethodPost, APIPrefix+"/auth/login",
		`{"username":"admin","password":"definitely-not-the-Password1"}`, "")
	unknownUser := postJSON(t, env.raw, http.MethodPost, APIPrefix+"/auth/login",
		`{"username":"nobody","password":"definitely-not-the-Password1"}`, "")

	if wrongPassword.Code != http.StatusUnauthorized || unknownUser.Code != http.StatusUnauthorized {
		t.Fatalf("statuses = %d / %d, want both 401", wrongPassword.Code, unknownUser.Code)
	}

	// Revealing which one failed would turn the login form into an account
	// enumeration oracle.
	wrongCode, wrongMsg := errorOf(t, wrongPassword)
	unknownCode, unknownMsg := errorOf(t, unknownUser)
	if wrongCode != unknownCode || wrongMsg != unknownMsg {
		t.Errorf("responses differ: %q/%q vs %q/%q", wrongCode, wrongMsg, unknownCode, unknownMsg)
	}
	if wrongCode != string(httpx.CodeInvalidCredentials) {
		t.Errorf("code = %q, want %q", wrongCode, httpx.CodeInvalidCredentials)
	}
}

func TestRefreshRotatesTheToken(t *testing.T) {
	env := newEnv(t, fake.New())

	login := postJSON(t, env.raw, http.MethodPost, APIPrefix+"/auth/login",
		`{"username":"admin","password":"`+testPassword+`"}`, "")
	first := decodeSession(t, login)
	refreshToken, _ := first["refresh_token"].(string)

	rec := postJSON(t, env.raw, http.MethodPost, APIPrefix+"/auth/refresh",
		`{"refresh_token":"`+refreshToken+`"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	second := decodeSession(t, rec)
	newRefresh, _ := second["refresh_token"].(string)
	if newRefresh == "" || newRefresh == refreshToken {
		t.Fatal("refresh did not rotate the token")
	}

	// The old token must be dead: a stolen copy stops working as soon as the
	// real user refreshes.
	replay := postJSON(t, env.raw, http.MethodPost, APIPrefix+"/auth/refresh",
		`{"refresh_token":"`+refreshToken+`"}`, "")
	if replay.Code != http.StatusUnauthorized {
		t.Errorf("replaying the old refresh token returned %d, want 401", replay.Code)
	}

	// The new one must work.
	again := postJSON(t, env.raw, http.MethodPost, APIPrefix+"/auth/refresh",
		`{"refresh_token":"`+newRefresh+`"}`, "")
	if again.Code != http.StatusOK {
		t.Errorf("the rotated token was rejected: %d", again.Code)
	}
}

func TestRefreshRejectsUnknownTokens(t *testing.T) {
	env := newEnv(t, fake.New())

	for _, token := range []string{"", "made-up-token"} {
		rec := postJSON(t, env.raw, http.MethodPost, APIPrefix+"/auth/refresh",
			`{"refresh_token":"`+token+`"}`, "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("token %q: status = %d, want 401", token, rec.Code)
		}
	}
}

func TestLogoutRevokesTheSession(t *testing.T) {
	env := newEnv(t, fake.New())

	login := postJSON(t, env.raw, http.MethodPost, APIPrefix+"/auth/login",
		`{"username":"admin","password":"`+testPassword+`"}`, "")
	session := decodeSession(t, login)
	accessToken, _ := session["access_token"].(string)
	refreshToken, _ := session["refresh_token"].(string)

	rec := postJSON(t, env.raw, http.MethodPost, APIPrefix+"/auth/logout",
		`{"refresh_token":"`+refreshToken+`"}`, accessToken)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}

	after := postJSON(t, env.raw, http.MethodPost, APIPrefix+"/auth/refresh",
		`{"refresh_token":"`+refreshToken+`"}`, "")
	if after.Code != http.StatusUnauthorized {
		t.Errorf("the revoked refresh token still works: %d", after.Code)
	}
}

func TestMeDescribesTheCaller(t *testing.T) {
	env := newEnv(t, fake.New())

	rec := request(t, env.as(store.RoleOperator), http.MethodGet, APIPrefix+"/auth/me")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	body := decodeSession(t, rec)
	if body["username"] != "operator" || body["role"] != string(store.RoleOperator) {
		t.Errorf("body = %v", body)
	}

	perms, ok := body["permissions"].([]any)
	if !ok || len(perms) == 0 {
		t.Fatalf("permissions = %v, want the caller's capability list", body["permissions"])
	}
	// The UI hides controls based on this list, so it must not claim more than
	// the role actually has.
	for _, p := range perms {
		if p == "admin" || p == "build" {
			t.Errorf("operator was told it has %v", p)
		}
	}
}

func TestDisabledAccountCannotUseAnExistingToken(t *testing.T) {
	env := newEnv(t, fake.New())
	ctx := context.Background()

	user, err := env.db.Users.ByUsername(ctx, "viewer")
	if err != nil {
		t.Fatalf("ByUsername() error = %v", err)
	}
	if err := env.db.Users.SetDisabled(ctx, user.ID, true); err != nil {
		t.Fatalf("SetDisabled() error = %v", err)
	}

	// The token was valid when issued; disabling the account must take effect
	// immediately rather than at expiry.
	rec := request(t, env.as(store.RoleViewer), http.MethodGet, APIPrefix+"/containers")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if code, _ := errorOf(t, rec); code != string(httpx.CodeAccountDisabled) {
		t.Errorf("code = %q, want %q", code, httpx.CodeAccountDisabled)
	}
}

func TestDeletedAccountCannotUseAnExistingToken(t *testing.T) {
	env := newEnv(t, fake.New())
	ctx := context.Background()

	user, err := env.db.Users.ByUsername(ctx, "viewer")
	if err != nil {
		t.Fatalf("ByUsername() error = %v", err)
	}
	if err := env.db.Users.Delete(ctx, user.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	rec := request(t, env.as(store.RoleViewer), http.MethodGet, APIPrefix+"/containers")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for a deleted account", rec.Code)
	}
}

func TestBruteForceLockoutAppliesToLogin(t *testing.T) {
	env := newEnv(t, fake.New())

	// The login rate limiter allows a small burst; exhaust it and keep going
	// until the database-backed lockout answers.
	var lastCode int
	var lockedOut bool
	for i := 0; i < 25; i++ {
		rec := postJSON(t, env.raw, http.MethodPost, APIPrefix+"/auth/login",
			`{"username":"admin","password":"wrong-password-attempt-1"}`, "")
		lastCode = rec.Code
		if rec.Code == http.StatusTooManyRequests {
			lockedOut = true
			break
		}
	}

	if !lockedOut {
		t.Fatalf("repeated failures were never throttled; last status = %d", lastCode)
	}
}

func TestCSRFGuardRejectsCrossOriginWrites(t *testing.T) {
	env := newEnv(t, fake.New())

	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/containers/"+runningID+"/start", nil)
	req.Header.Set("Authorization", "Bearer "+env.tokens[store.RoleAdmin])
	req.Header.Set("Origin", "https://evil.example")

	rec := httptest.NewRecorder()
	env.raw.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a cross-origin write", rec.Code)
	}
	if code, _ := errorOf(t, rec); code != string(httpx.CodeCSRFInvalid) {
		t.Errorf("code = %q, want %q", code, httpx.CodeCSRFInvalid)
	}
}

func TestCSRFGuardAllowsSameOriginWrites(t *testing.T) {
	env := newEnv(t, fake.New())

	req := httptest.NewRequest(http.MethodPost, APIPrefix+"/containers/"+runningID+"/start", nil)
	req.Header.Set("Authorization", "Bearer "+env.tokens[store.RoleAdmin])
	req.Header.Set("Origin", "http://"+req.Host)

	rec := httptest.NewRecorder()
	env.raw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want a same-origin write to succeed: %s", rec.Code, rec.Body.String())
	}
}

func TestCSRFGuardIgnoresReads(t *testing.T) {
	env := newEnv(t, fake.New())

	req := httptest.NewRequest(http.MethodGet, APIPrefix+"/containers", nil)
	req.Header.Set("Authorization", "Bearer "+env.tokens[store.RoleAdmin])
	req.Header.Set("Origin", "https://evil.example")

	rec := httptest.NewRecorder()
	env.raw.ServeHTTP(rec, req)

	// Reads change nothing, and the token cannot be attached by a cross-site
	// form anyway.
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want reads to pass the CSRF guard", rec.Code)
	}
}

func TestMalformedJSONIsRejected(t *testing.T) {
	h := newBareServer(t)

	tests := map[string]string{
		"broken":        `{"username":`,
		"empty body":    ``,
		"unknown field": `{"username":"a","password":"b","role":"admin"}`,
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			rec := postJSON(t, h, http.MethodPost, APIPrefix+"/auth/bootstrap", body, "")
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
		})
	}
}

func TestAPITokenAuthenticates(t *testing.T) {
	env := newEnv(t, fake.New())
	ctx := context.Background()

	user, err := env.db.Users.ByUsername(ctx, "operator")
	if err != nil {
		t.Fatalf("ByUsername() error = %v", err)
	}

	token, prefix, hash, err := newAPITokenFor(t)
	if err != nil {
		t.Fatalf("token generation error = %v", err)
	}
	id, _ := newTokenID(t)

	err = env.db.Tokens.Create(ctx, store.APIToken{
		ID: id, UserID: user.ID, Name: "ci", Prefix: prefix, TokenHash: hash,
	})
	if err != nil {
		t.Fatalf("Tokens.Create() error = %v", err)
	}

	rec := request(t, env.withToken(token), http.MethodGet, APIPrefix+"/auth/me")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want the API token to authenticate: %s", rec.Code, rec.Body.String())
	}

	body := decodeSession(t, rec)
	if body["username"] != "operator" {
		t.Errorf("username = %v", body["username"])
	}
	// The response identifies which token acted, so a leaked one can be traced.
	if body["token_id"] != id {
		t.Errorf("token_id = %v, want %q", body["token_id"], id)
	}

	// The token carries its user's role, not more.
	if rec := request(t, env.withToken(token), http.MethodGet, APIPrefix+"/containers"); rec.Code != http.StatusOK {
		t.Errorf("operator token could not read containers: %d", rec.Code)
	}
}

func TestRevokedAPITokenIsRejected(t *testing.T) {
	env := newEnv(t, fake.New())
	ctx := context.Background()

	user, _ := env.db.Users.ByUsername(ctx, "operator")
	token, prefix, hash, _ := newAPITokenFor(t)
	id, _ := newTokenID(t)

	err := env.db.Tokens.Create(ctx, store.APIToken{
		ID: id, UserID: user.ID, Name: "ci", Prefix: prefix, TokenHash: hash,
	})
	if err != nil {
		t.Fatalf("Tokens.Create() error = %v", err)
	}
	if err := env.db.Tokens.Revoke(ctx, id); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}

	rec := request(t, env.withToken(token), http.MethodGet, APIPrefix+"/auth/me")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want a revoked token to be rejected", rec.Code)
	}
}
