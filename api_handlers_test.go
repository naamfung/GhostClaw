package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ======================== Helper Functions ========================

// testHTTPServer returns a minimal HTTPServer for testing handler methods.
func testHTTPServer() *HTTPServer {
	return &HTTPServer{}
}

// saveRestoreGlobals saves key global variables and returns a cleanup
// function that restores them. Use this in tests that modify globals.
func saveRestoreGlobals() func() {
	oldConfigMgr := globalConfigManager
	oldRoleMgr := globalRoleManager
	oldSkillMgr := globalSkillManager
	oldSkillMgrV2 := globalSkillManagerV2
	oldActorMgr := globalActorManager
	oldHookMgr := globalHookManager
	oldTimeout := globalTimeoutConfig
	oldCompressionMode := globalCompressionMode
	oldCompressionThresh := globalCompressionThreshold
	oldSkillCleanup := globalSkillCleanupThresholdDays
	oldEscalation := globalEscalationThreshold
	oldDefaultRole := getDefaultRole()

	return func() {
		globalConfigManager = oldConfigMgr
		globalRoleManager = oldRoleMgr
		globalSkillManager = oldSkillMgr
		globalSkillManagerV2 = oldSkillMgrV2
		globalActorManager = oldActorMgr
		globalHookManager = oldHookMgr
		globalTimeoutConfig = oldTimeout
		globalCompressionMode = oldCompressionMode
		globalCompressionThreshold = oldCompressionThresh
		globalSkillCleanupThresholdDays = oldSkillCleanup
		globalEscalationThreshold = oldEscalation
		setDefaultRole(oldDefaultRole)
	}
}

// setupTestConfigManager creates a ConfigManager with a temp config and sets
// globalConfigManager. The returned cleanup function restores original globals.
func setupTestConfigManager(t *testing.T) *ConfigManager {
	t.Helper()
	cleanup := saveRestoreGlobals()
	t.Cleanup(cleanup)

	tmpDir := t.TempDir()
	cm, err := NewConfigManager(tmpDir)
	if err != nil {
		t.Fatalf("failed to create ConfigManager: %v", err)
	}
	globalConfigManager = cm
	return cm
}

// ======================== Category 1: Standalone Utility Functions ========================

func TestMaskAPIKey_ShortKey(t *testing.T) {
	// Keys with length <= 8 return just "****"
	result := maskAPIKey("short")
	if result != "****" {
		t.Errorf("maskAPIKey(short) = %q, want %q", result, "****")
	}
}

func TestMaskAPIKey_ExactlyEightChars(t *testing.T) {
	result := maskAPIKey("12345678")
	if result != "****" {
		t.Errorf("maskAPIKey(12345678) = %q, want %q", result, "****")
	}
}

func TestMaskAPIKey_LongKey(t *testing.T) {
	result := maskAPIKey("sk-1234567890abcdef")
	if result != "sk-1****cdef" {
		t.Errorf("maskAPIKey(long) = %q, want %q", result, "sk-1****cdef")
	}
}

func TestMaskAPIKey_EmptyKey(t *testing.T) {
	result := maskAPIKey("")
	if result != "****" {
		t.Errorf("maskAPIKey(empty) = %q, want %q", result, "****")
	}
}

func TestMaskAPIKey_VeryLongKey(t *testing.T) {
	result := maskAPIKey("sk-abcdefghijklmnopqrstuvwxyz1234567890")
	if result != "sk-a****7890" {
		t.Errorf("maskAPIKey(very_long) = %q, want %q", result, "sk-a****7890")
	}
}

func TestCORS_OptionsReturnsTrue(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodOptions, "/api/config", nil)

	result := handleCORS(w, r)
	if !result {
		t.Error("handleCORS(OPTIONS) should return true")
	}
	if w.Code != http.StatusOK {
		t.Errorf("handleCORS(OPTIONS) status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("handleCORS(OPTIONS) should set CORS headers")
	}
}

func TestCORS_GetReturnsFalse(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/config", nil)

	result := handleCORS(w, r)
	if result {
		t.Error("handleCORS(GET) should return false")
	}
	if w.Code != http.StatusOK {
		// StatusOK is the default (0), WriteHeader hasn't been called
	}
}

func TestCORS_PostReturnsFalse(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/config", nil)

	result := handleCORS(w, r)
	if result {
		t.Error("handleCORS(POST) should return false")
	}
}

func TestCommonHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	setCommonHeaders(w)

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if origin := w.Header().Get("Access-Control-Allow-Origin"); origin != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", origin)
	}
	if methods := w.Header().Get("Access-Control-Allow-Methods"); methods != "GET, POST, PUT, DELETE, OPTIONS" {
		t.Errorf("Access-Control-Allow-Methods = %q, want GET, POST, PUT, DELETE, OPTIONS", methods)
	}
	if headers := w.Header().Get("Access-Control-Allow-Headers"); headers != "Content-Type" {
		t.Errorf("Access-Control-Allow-Headers = %q, want Content-Type", headers)
	}
}

func TestDefaultRole_RoundTrip(t *testing.T) {
	old := getDefaultRole()
	defer setDefaultRole(old)

	// Set and verify
	setDefaultRole("coder")
	if got := getDefaultRole(); got != "coder" {
		t.Errorf("getDefaultRole() = %q, want coder", got)
	}

	// Change it
	setDefaultRole("reviewer")
	if got := getDefaultRole(); got != "reviewer" {
		t.Errorf("getDefaultRole() after change = %q, want reviewer", got)
	}
}

func TestDefaultRole_EmptyString(t *testing.T) {
	old := getDefaultRole()
	defer setDefaultRole(old)

	setDefaultRole("")
	if got := getDefaultRole(); got != "" {
		t.Errorf("getDefaultRole() with empty string = %q, want empty", got)
	}
}

// ======================== Category 2: Handler Error/Method-Routing Paths ========================

// --- configHandler ---

func TestConfigHandler_MethodNotAllowed(t *testing.T) {
	s := testHTTPServer()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/config", nil)

	s.configHandler(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
	body := w.Body.String()
	if !contains(body, "方法不允许") {
		t.Errorf("body should contain '方法不允许', got %q", body)
	}
}

func TestConfigHandler_Options(t *testing.T) {
	s := testHTTPServer()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodOptions, "/api/config", nil)

	s.configHandler(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("OPTIONS status = %d, want %d", w.Code, http.StatusOK)
	}
}

// --- rolesHandler ---

func TestRolesHandler_MethodNotAllowed(t *testing.T) {
	s := testHTTPServer()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/roles", nil)

	s.rolesHandler(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestRolesHandler_ManagerNil_Get(t *testing.T) {
	s := testHTTPServer()
	cleanup := saveRestoreGlobals()
	globalRoleManager = nil
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/roles", nil)

	s.rolesHandler(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if !contains(w.Body.String(), "人格管理器未初始化") {
		t.Errorf("body should contain nil-manager error, got %q", w.Body.String())
	}
}

func TestRolesHandler_ManagerNil_Post(t *testing.T) {
	s := testHTTPServer()
	cleanup := saveRestoreGlobals()
	globalRoleManager = nil
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/roles", nil)

	s.rolesHandler(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// --- rolesDetailHandler ---

func TestRoleDetailHandler_EmptyName(t *testing.T) {
	s := testHTTPServer()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/roles/", nil)

	s.roleDetailHandler(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if !contains(w.Body.String(), "人格名称不能为空") {
		t.Errorf("body should contain empty-name error, got %q", w.Body.String())
	}
}

func TestRoleDetailHandler_MethodNotAllowed(t *testing.T) {
	s := testHTTPServer()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPatch, "/api/roles/testrole", nil)

	s.roleDetailHandler(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// --- skillsHandler ---

func TestSkillsHandler_MethodNotAllowed(t *testing.T) {
	s := testHTTPServer()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/skills", nil)

	s.skillsHandler(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestSkillsHandler_ManagerNil_Get(t *testing.T) {
	s := testHTTPServer()
	cleanup := saveRestoreGlobals()
	globalSkillManager = nil
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/skills", nil)

	s.skillsHandler(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if !contains(w.Body.String(), "技能管理器未初始化") {
		t.Errorf("body should contain nil-manager error, got %q", w.Body.String())
	}
}

// --- skillDetailHandler ---

func TestSkillDetailHandler_EmptyName(t *testing.T) {
	s := testHTTPServer()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/skills/", nil)

	s.skillDetailHandler(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if !contains(w.Body.String(), "技能名称不能为空") {
		t.Errorf("body should contain empty-name error, got %q", w.Body.String())
	}
}

func TestSkillDetailHandler_MethodNotAllowed(t *testing.T) {
	s := testHTTPServer()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPatch, "/api/skills/testskill", nil)

	s.skillDetailHandler(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// --- actorsHandler ---

func TestActorsHandler_MethodNotAllowed(t *testing.T) {
	s := testHTTPServer()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/actors", nil)

	s.actorsHandler(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestActorsHandler_ManagerNil_Get(t *testing.T) {
	s := testHTTPServer()
	cleanup := saveRestoreGlobals()
	globalActorManager = nil
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/actors", nil)

	s.actorsHandler(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if !contains(w.Body.String(), "演员管理器未初始化") {
		t.Errorf("body should contain nil-manager error, got %q", w.Body.String())
	}
}

// --- actorDetailHandler ---

func TestActorDetailHandler_EmptyName(t *testing.T) {
	s := testHTTPServer()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/actors/", nil)

	s.actorDetailHandler(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if !contains(w.Body.String(), "演员名称不能为空") {
		t.Errorf("body should contain empty-name error, got %q", w.Body.String())
	}
}

func TestActorDetailHandler_MethodNotAllowed(t *testing.T) {
	s := testHTTPServer()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPatch, "/api/actors/testactor", nil)

	s.actorDetailHandler(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// --- newSessionHandler ---

func TestNewSessionHandler_MethodNotAllowed(t *testing.T) {
	s := testHTTPServer()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/session/new", nil)

	s.newSessionHandler(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
	if !contains(w.Body.String(), "方法不允许") {
		t.Errorf("body should contain method-not-allowed error, got %q", w.Body.String())
	}
}

func TestNewSessionHandler_Options(t *testing.T) {
	s := testHTTPServer()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodOptions, "/api/session/new", nil)

	s.newSessionHandler(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("OPTIONS status = %d, want %d", w.Code, http.StatusOK)
	}
}

// --- modelsAPIHandler ---

func TestModelsHandler_MethodNotAllowed(t *testing.T) {
	s := testHTTPServer()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/models", nil)

	s.modelsAPIHandler(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestModelsHandler_ManagerNil_Get(t *testing.T) {
	s := testHTTPServer()
	cleanup := saveRestoreGlobals()
	globalConfigManager = nil
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/models", nil)

	s.modelsAPIHandler(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if !contains(w.Body.String(), "配置管理器未初始化") {
		t.Errorf("body should contain nil-manager error, got %q", w.Body.String())
	}
}

func TestModelsHandler_ManagerNil_Post(t *testing.T) {
	s := testHTTPServer()
	cleanup := saveRestoreGlobals()
	globalConfigManager = nil
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/models", nil)

	s.modelsAPIHandler(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// --- modelDetailHandler ---

func TestModelDetailHandler_EmptyName(t *testing.T) {
	s := testHTTPServer()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/models/", nil)

	s.modelDetailHandler(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if !contains(w.Body.String(), "模型名称不能为空") {
		t.Errorf("body should contain empty-name error, got %q", w.Body.String())
	}
}

func TestModelDetailHandler_MethodNotAllowed(t *testing.T) {
	s := testHTTPServer()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPatch, "/api/models/testmodel", nil)

	s.modelDetailHandler(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestModelDetailHandler_SetMain_WrongMethod(t *testing.T) {
	s := testHTTPServer()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/models/testmodel/set-main", nil)

	s.modelDetailHandler(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET set-main status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// --- hooksHandler ---

func TestHooksHandler_MethodNotAllowed(t *testing.T) {
	s := testHTTPServer()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/hooks", nil)

	s.hooksHandler(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHooksHandler_ManagerNil_Get(t *testing.T) {
	s := testHTTPServer()
	cleanup := saveRestoreGlobals()
	globalHookManager = nil
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/hooks", nil)

	s.hooksHandler(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if !contains(w.Body.String(), "Hook 管理器未初始化") {
		t.Errorf("body should contain nil-manager error, got %q", w.Body.String())
	}
}

// --- hookDetailHandler ---

func TestHookDetailHandler_EmptyPath(t *testing.T) {
	s := testHTTPServer()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/hooks/", nil)

	s.hookDetailHandler(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if !contains(w.Body.String(), "Hook 名称不能为空") {
		t.Errorf("body should contain empty-name error, got %q", w.Body.String())
	}
}

func TestHookDetailHandler_MethodNotAllowed(t *testing.T) {
	s := testHTTPServer()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/hooks/testhook", nil)

	s.hookDetailHandler(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHookDetailHandler_Reload_WrongMethod(t *testing.T) {
	s := testHTTPServer()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/hooks/reload", nil)

	s.hookDetailHandler(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET reload status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

// ======================== Category 3: Manager-Dependent Success Paths ========================

// --- GET /api/config ---

func TestGetConfig_Success(t *testing.T) {
	s := testHTTPServer()
	_ = setupTestConfigManager(t)

	// Set up required global variables that getConfig reads
	globalTimeoutConfig = TimeoutConfig{
		Shell:   120,
		HTTP:    60,
		Plugin:  30,
		Browser: 45,
	}
	globalCompressionMode = "token"
	globalCompressionThreshold = 0.8
	globalSkillCleanupThresholdDays = 90
	globalEscalationThreshold = 3
	setDefaultRole("coder")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/config", nil)

	s.getConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify response structure
	for _, key := range []string{"APIConfig", "DefaultRole", "NeedsSetup", "Timeout", "Compression", "SkillCleanupThresholdDays", "EscalationThreshold"} {
		if _, ok := resp[key]; !ok {
			t.Errorf("response missing key %q", key)
		}
	}

	// Verify API key is masked (default model has no API key, so needsSetup is true)
	apiCfg, ok := resp["APIConfig"].(map[string]interface{})
	if !ok {
		t.Fatal("APIConfig is not a map")
	}
	if _, ok := apiCfg["APIKey"]; !ok {
		t.Error("APIConfig missing APIKey")
	}
	// Default role should be "coder" since we set it
	if resp["DefaultRole"] != "coder" {
		t.Errorf("DefaultRole = %v, want coder", resp["DefaultRole"])
	}
	// NeedsSetup should be true since default model has no API key
	if resp["NeedsSetup"] != true {
		t.Errorf("NeedsSetup = %v, want true", resp["NeedsSetup"])
	}
}

func TestGetConfig_TimeoutValues(t *testing.T) {
	s := testHTTPServer()
	_ = setupTestConfigManager(t)

	globalTimeoutConfig = TimeoutConfig{
		Shell:   300,
		HTTP:    120,
		Plugin:  60,
		Browser: 90,
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/config", nil)

	s.getConfig(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	timeout, ok := resp["Timeout"].(map[string]interface{})
	if !ok {
		t.Fatal("Timeout is not a map")
	}
	if shell := timeout["Shell"]; shell != float64(300) {
		t.Errorf("Timeout.Shell = %v, want 300", shell)
	}
	if httpVal := timeout["HTTP"]; httpVal != float64(120) {
		t.Errorf("Timeout.HTTP = %v, want 120", httpVal)
	}
}

// --- GET /api/models ---

func TestListModelsAPI_Success(t *testing.T) {
	s := testHTTPServer()
	_ = setupTestConfigManager(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/models", nil)

	s.listModelsAPI(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify response structure
	if _, ok := resp["Models"]; !ok {
		t.Error("response missing 'Models' key")
	}
	if _, ok := resp["MainModel"]; !ok {
		t.Error("response missing 'MainModel' key")
	}

	models, ok := resp["Models"].([]interface{})
	if !ok {
		t.Fatal("Models is not an array")
	}
	if len(models) == 0 {
		t.Error("expected at least one model in default config")
	}

	// Check that the first model has expected fields
	firstModel, ok := models[0].(map[string]interface{})
	if !ok {
		t.Fatal("first model is not a map")
	}
	requiredFields := []string{"Name", "APIType", "BaseURL", "APIKey", "Model", "IsDefault"}
	for _, field := range requiredFields {
		if _, ok := firstModel[field]; !ok {
			t.Errorf("model missing field %q", field)
		}
	}
	// API key should be empty for security
	if firstModel["APIKey"] != "" {
		t.Errorf("APIKey should be empty, got %v", firstModel["APIKey"])
	}
	// Default model should have IsDefault = true
	if firstModel["IsDefault"] != true {
		t.Errorf("default model IsDefault = %v, want true", firstModel["IsDefault"])
	}
}

func TestListModelsAPI_NilManager(t *testing.T) {
	s := testHTTPServer()
	cleanup := saveRestoreGlobals()
	globalConfigManager = nil
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/models", nil)

	s.listModelsAPI(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// --- GET /api/config via configHandler ---

func TestConfigHandler_Get_Success(t *testing.T) {
	s := testHTTPServer()
	_ = setupTestConfigManager(t)

	globalTimeoutConfig = TimeoutConfig{Shell: 120, HTTP: 60, Plugin: 30, Browser: 45}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/config", nil)

	s.configHandler(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	// Basic sanity: check NeedsSetup is present (default model has no API key)
	if _, ok := resp["NeedsSetup"]; !ok {
		t.Error("response missing 'NeedsSetup' key")
	}
}

// --- GET /api/models via modelsAPIHandler ---

func TestModelsHandler_Get_Success(t *testing.T) {
	s := testHTTPServer()
	_ = setupTestConfigManager(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/models", nil)

	s.modelsAPIHandler(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	if _, ok := resp["Models"]; !ok {
		t.Error("response missing 'Models'")
	}
}

// --- POST /api/session/new (integration) ---

func TestNewSessionHandler_PostSuccess(t *testing.T) {
	s := testHTTPServer()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/session/new", nil)

	s.newSessionHandler(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := resp["message"]; !ok {
		t.Error("response missing 'message' key")
	}
}

// --- GET /api/actors with empty Model fill-in ---

func TestListActors_EmptyModelFillsDefault(t *testing.T) {
	s := testHTTPServer()
	cleanup := saveRestoreGlobals()
	defer cleanup()

	// 建立 ConfigManager（默认模型名 "deepseek-chat"）
	tmpDir := t.TempDir()
	cm, err := NewConfigManager(tmpDir)
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}
	globalConfigManager = cm

	// 建立 ActorManager，内部会创建一个 Model 为空的默认演员
	actorFile := tmpDir + "/actors.toon"
	am, err := NewActorManager(actorFile, "coder")
	if err != nil {
		t.Fatalf("NewActorManager: %v", err)
	}
	globalActorManager = am

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/actors", nil)
	s.listActors(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	actors, ok := resp["Actors"].([]interface{})
	if !ok || len(actors) == 0 {
		t.Fatal("Actors is empty")
	}

	// 默认演员的 Model 应该被填充为默认模型名
	defaultActor := actors[0].(map[string]interface{})
	if model, _ := defaultActor["Model"].(string); model == "" {
		t.Error("default actor Model should not be empty, expected filled-in main model name")
	}
	mainModel := cm.GetMainModelName()
	if model, _ := defaultActor["Model"].(string); model != mainModel {
		t.Errorf("default actor Model = %q, want %q (main model)", model, mainModel)
	}
}

func TestListActors_ExistingModelPreserved(t *testing.T) {
	s := testHTTPServer()
	cleanup := saveRestoreGlobals()
	defer cleanup()

	tmpDir := t.TempDir()
	cm, err := NewConfigManager(tmpDir)
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}
	globalConfigManager = cm

	actorFile := tmpDir + "/actors.toon"
	am, err := NewActorManager(actorFile, "coder")
	if err != nil {
		t.Fatalf("NewActorManager: %v", err)
	}
	// 添加一个有明确 Model 的演员
	am.AddActor(&Actor{
		Name:          "custom_actor",
		Role:          "coder",
		Model:         "gpt-4",
		CharacterName: "Custom",
		Description:   "A custom actor",
	})
	globalActorManager = am

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/actors", nil)
	s.listActors(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	actors := resp["Actors"].([]interface{})

	for _, a := range actors {
		actor := a.(map[string]interface{})
		name, _ := actor["Name"].(string)
		model, _ := actor["Model"].(string)
		if name == "custom_actor" && model != "gpt-4" {
			t.Errorf("custom_actor Model = %q, want %q (should preserve explicit model)", model, "gpt-4")
		}
	}
}

// ======================== Helper: contains ========================

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

