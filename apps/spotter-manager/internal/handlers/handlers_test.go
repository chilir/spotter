package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubetesting "k8s.io/client-go/testing"
)

func TestServeFrontend(t *testing.T) {
	tmpDir := t.TempDir()
	webDir := filepath.Join(tmpDir, "web")
	if err := os.Mkdir(webDir, 0755); err != nil {
		t.Fatalf("Failed to create temporary web dir: %v", err)
	}
	indexHTMLPath := filepath.Join(webDir, "index.html")
	if err := os.WriteFile(indexHTMLPath, []byte("<html>Test</html>"), 0644); err != nil {
		t.Fatalf("Failed to write temporary index.html: %v", err)
	}

	// Change working directory for the test so ServeFile finds the relative path
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current working directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change current working directory: %v", err)
	}
	defer os.Chdir(originalWD)

	req, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(ServeFrontend)

	handler.ServeHTTP(rr, req)

	// Check status code
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	// Check cache headers
	expectedHeaders := map[string]string{
		"Cache-Control": "no-cache, no-store, must-revalidate",
		"Pragma":        "no-cache",
		"Expires":       "0",
	}
	for key, expectedValue := range expectedHeaders {
		if value := rr.Header().Get(key); value != expectedValue {
			t.Errorf("Handler returned wrong header %s: got %v want %v", key, value, expectedValue)
		}
	}

	// Check body
	if !strings.Contains(rr.Body.String(), "<html>Test</html>") {
		t.Errorf("Handler returned unexpected body: got %v", rr.Body.String())
	}
}

func TestMakeDeployHandler(t *testing.T) {
	rayGVR := schema.GroupVersionResource{
		Group:    "ray.io",
		Version:  "v1alpha1",
		Resource: "rayservices",
	}

	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "configs")
	if err := os.Mkdir(configDir, 0755); err != nil {
		t.Fatalf("Failed to create temporary configs dir: %v", err)
	}
	validTemplateContent := `
apiVersion: ray.io/v1alpha1
kind: RayService
metadata:
  name: spotter-ray-service
spec:
  rayClusterConfig:
    headGroupSpec:
      template:
        spec:
          containers:
            - name: ray-head
              image: {{.DockerImage}}
`

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current working directory: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change current working directory: %v", err)
	}
	defer os.Chdir(originalWD)

	tests := []struct {
		name               string
		method             string
		imageQueryParam    string
		mockTemplatePath   string // Use relative path from tmpDir
		setupFakeClient    func() *dynamicfake.FakeDynamicClient
		expectedStatusCode int
		expectedBody       string
		checkK8sActions    func(t *testing.T, actions []kubetesting.Action)
	}{
		{
			name:             "Success",
			method:           http.MethodPost,
			imageQueryParam:  "test-image:latest",
			mockTemplatePath: "configs/rayservice-template.yaml",
			setupFakeClient: func() *dynamicfake.FakeDynamicClient {
				client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
				client.PrependReactor("patch", "rayservices", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
					patchAction, ok := action.(kubetesting.PatchAction)
					if !ok {
						// Should not happen if reactor is correctly mapped to 'patch'
						return false, nil, fmt.Errorf("unexpected action type %T in patch reactor", action)
					}
					if patchAction.GetPatchType() != types.ApplyPatchType {
						// Let non-apply patches pass through or handle differently if needed
						return false, nil, nil
					}

					// Simulate successful apply: return an object with a UID
					// The object content comes from the patch itself, which checkK8sActions will verify
					retObj := &unstructured.Unstructured{}
					err = json.Unmarshal(patchAction.GetPatch(), &retObj.Object)
					if err != nil {
						return true, nil, fmt.Errorf("failed to unmarshal apply patch in reactor: %w", err)
					}
					// Ensure basic metadata matches the action
					retObj.SetName(patchAction.GetName())
					retObj.SetNamespace(patchAction.GetNamespace())
					retObj.SetUID("test-uid-123") // Simulate UID generation
					// Set GVK based on the resource being patched
					gvk := action.GetResource().GroupVersion().WithKind("RayService") // Assuming Kind based on Resource
					retObj.SetGroupVersionKind(gvk)

					return true, retObj, nil
				})
				return client
			},
			expectedStatusCode: http.StatusOK,
			expectedBody:       fmt.Sprintf("RayService '%s' applied successfully in namespace '%s'", rayServiceName, rayServiceNamespace),
			checkK8sActions: func(t *testing.T, actions []kubetesting.Action) {
				if len(actions) != 1 {
					t.Errorf("Expected 1 K8s action, got %d", len(actions))
					return
				}
				action := actions[0]
				patchAction, ok := action.(kubetesting.PatchAction)
				if !ok {
					t.Fatalf("Expected PatchAction, got %T", action)
				}
				if patchAction.GetVerb() != "patch" {
					t.Errorf("Expected verb 'patch', got '%s'", patchAction.GetVerb())
				}
				if patchAction.GetPatchType() != types.ApplyPatchType {
					t.Errorf("Expected patch type Apply, got %s", patchAction.GetPatchType())
				}
				if patchAction.GetResource() != rayGVR {
					t.Errorf("Unexpected K8s resource: %s", patchAction.GetResource())
				}
				if patchAction.GetName() != rayServiceName {
					t.Errorf(
						"Expected apply for name %s, got %s",
						rayServiceName,
						patchAction.GetName(),
					)
				}

				// Inspect the patch data
				patchBytes := patchAction.GetPatch()
				appliedObj := &unstructured.Unstructured{}
				err := json.Unmarshal(patchBytes, &appliedObj.Object)
				if err != nil {
					t.Fatalf(
						"Failed to unmarshal patch data: %v\nData: %s",
						err,
						string(patchBytes),
					)
				}

				// Log the unmarshalled object structure for debugging
				t.Logf("Unmarshalled Patch Object: %#v", appliedObj.Object)

				// Check important fields within the patch data
				containers, found, err := unstructured.NestedSlice(
					appliedObj.Object,
					"spec",
					"rayClusterConfig",
					"headGroupSpec",
					"template",
					"spec",
					"containers",
				)
				if !found || err != nil {
					t.Fatalf("Failed to find containers slice: found=%v, err=%v", found, err)
				}
				if len(containers) == 0 {
					t.Fatalf("Containers slice is empty")
				}
				containerMap, ok := containers[0].(map[string]interface{})
				if !ok {
					t.Fatalf(
						"First element in containers slice is not a map[string]interface{}, got %T",
						containers[0],
					)
				}
				image, found, err := unstructured.NestedString(containerMap, "image")
				if !found || err != nil || image != "test-image:latest" {
					t.Errorf(
						"Expected image 'test-image:latest' in containerMap, got '%s' (found: %v, err: %v)",
						image,
						found,
						err,
					)
				}

				// Also check the name for good measure
				name, found, err := unstructured.NestedString(containerMap, "name")
				if !found || err != nil || name != "ray-head" {
					t.Errorf(
						"Expected name 'ray-head' in containerMap, got '%s' (found: %v, err: %v)",
						name,
						found,
						err,
					)
				}
			},
		},
		{
			name:             "Error - Missing Image Query Param",
			method:           http.MethodPost,
			imageQueryParam:  "",
			mockTemplatePath: "configs/rayservice-template.yaml",
			setupFakeClient: func() *dynamicfake.FakeDynamicClient {
				return dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
			},
			expectedStatusCode: http.StatusBadRequest,
			expectedBody:       "Missing required query parameter: dockerimage",
		},
		{
			name:             "Error - Wrong Method",
			method:           http.MethodGet,
			imageQueryParam:  "test-image:latest",
			mockTemplatePath: "configs/rayservice-template.yaml",
			setupFakeClient: func() *dynamicfake.FakeDynamicClient {
				return dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
			},
			expectedStatusCode: http.StatusMethodNotAllowed,
			expectedBody:       "Only POST requests are allowed.",
		},
		{
			name:             "Error - Template Not Found",
			method:           http.MethodPost,
			imageQueryParam:  "test-image:latest",
			mockTemplatePath: "configs/nonexistent-template.yaml", // Does not exist
			setupFakeClient: func() *dynamicfake.FakeDynamicClient {
				return dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedBody:       "Internal server error: RayService manifest template file missing",
		},
		{
			name:             "Error - K8s Apply Fails",
			method:           http.MethodPost,
			imageQueryParam:  "test-image:latest",
			mockTemplatePath: "configs/rayservice-template.yaml",
			setupFakeClient: func() *dynamicfake.FakeDynamicClient {
				client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
				client.PrependReactor("patch", "rayservices", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
					// Ensure it's an Apply patch before erroring
					patchAction, ok := action.(kubetesting.PatchAction)
					if !ok || patchAction.GetPatchType() != types.ApplyPatchType {
						return false, nil, nil // Let other patch types pass through
					}
					return true, nil, fmt.Errorf("simulated apply error")
				})
				return client
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedBody:       fmt.Sprintf("Internal server error: failed to apply RayService '%s' in namespace '%s': simulated apply error", rayServiceName, rayServiceNamespace),
			checkK8sActions: func(t *testing.T, actions []kubetesting.Action) {
				if len(actions) != 1 {
					t.Errorf("Expected 1 K8s action, got %d", len(actions))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "Success" || tt.name == "Error - K8s Apply Fails" || tt.name == "Error - Missing Image Query Param" {
				templatePath := filepath.Join(configDir, "rayservice-template.yaml")
				if err := os.WriteFile(templatePath, []byte(validTemplateContent), 0644); err != nil {
					t.Fatalf("Failed to write rayservice-template.yaml for test: %v", err)
				}
				t.Cleanup(func() { os.Remove(templatePath) })
			}

			fakeClient := tt.setupFakeClient()
			handler := MakeDeployHandler(fakeClient)

			req, err := http.NewRequest(tt.method, fmt.Sprintf("/deploy?dockerimage=%s", tt.imageQueryParam), nil)
			if err != nil {
				t.Fatal(err)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatusCode {
				t.Errorf("Expected status code %d, got %d. Body: %s", tt.expectedStatusCode, rr.Code, rr.Body.String())
			}

			if body := strings.TrimSpace(rr.Body.String()); body != tt.expectedBody {
				t.Errorf("Expected body '%s', got '%s'", tt.expectedBody, body)
			}

			if tt.checkK8sActions != nil {
				tt.checkK8sActions(t, fakeClient.Actions())
			} else if len(fakeClient.Actions()) > 0 && tt.expectedStatusCode < 400 {
				// Only check for no actions if success was expected but no checker provided
				t.Logf("Warning: K8s actions were performed but not checked for test '%s'", tt.name)
			} else if len(fakeClient.Actions()) > 0 && tt.expectedStatusCode >= 400 {
				// Don't check actions on expected error cases unless explicitly specified
			} else if tt.expectedStatusCode < 400 && len(fakeClient.Actions()) == 0 {
				t.Errorf("Expected K8s actions but none occurred for test '%s'", tt.name)
			}
		})
	}
}

func TestMakeDeleteHandler(t *testing.T) {
	rayGVR := schema.GroupVersionResource{Group: "ray.io", Version: "v1alpha1", Resource: "rayservices"}

	tests := []struct {
		name               string
		method             string
		setupFakeClient    func() *dynamicfake.FakeDynamicClient
		expectedStatusCode int
		expectedBody       string
		checkK8sActions    func(t *testing.T, actions []kubetesting.Action)
	}{
		{
			name:   "Success",
			method: http.MethodPost,
			setupFakeClient: func() *dynamicfake.FakeDynamicClient {
				client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
				// No specific reactor needed for delete success unless checking preconditions
				return client
			},
			expectedStatusCode: http.StatusOK,
			expectedBody:       fmt.Sprintf("RayService '%s' in namespace '%s' did not exist.", rayServiceName, rayServiceNamespace),
			checkK8sActions: func(t *testing.T, actions []kubetesting.Action) {
				if len(actions) != 1 {
					t.Fatalf("Expected 1 k8s action, got %d", len(actions))
				}
				action := actions[0]
				if action.GetVerb() != "delete" || action.GetResource() != rayGVR {
					t.Errorf("Unexpected k8s action: %s %s", action.GetVerb(), action.GetResource())
				}
				deleteAction := action.(kubetesting.DeleteAction)
				if deleteAction.GetName() != rayServiceName || deleteAction.GetNamespace() != rayServiceNamespace {
					t.Errorf("Expected delete for %s/%s, got %s/%s",
						rayServiceNamespace, rayServiceName, deleteAction.GetNamespace(), deleteAction.GetName())
				}
			},
		},
		{
			name:   "Error - K8s Delete Fails",
			method: http.MethodPost,
			setupFakeClient: func() *dynamicfake.FakeDynamicClient {
				client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
				client.PrependReactor("delete", "rayservices", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, fmt.Errorf("simulated delete error")
				})
				return client
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedBody:       fmt.Sprintf("Internal server error: failed to delete RayService '%s' in namespace '%s': simulated delete error", rayServiceName, rayServiceNamespace),
			checkK8sActions: func(t *testing.T, actions []kubetesting.Action) {
				if len(actions) != 1 {
					t.Fatalf("Expected 1 k8s action, got %d", len(actions))
				}
				// Verification of the delete attempt is implicit in the error message check
			},
		},
		{
			name:               "Error - Wrong Method",
			method:             http.MethodGet,
			setupFakeClient:    func() *dynamicfake.FakeDynamicClient { return dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()) },
			expectedStatusCode: http.StatusMethodNotAllowed,
			expectedBody:       "Only POST requests are allowed.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := tt.setupFakeClient()
			handler := MakeDeleteHandler(fakeClient)

			req, err := http.NewRequest(tt.method, "/delete", nil)
			if err != nil {
				t.Fatal(err)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatusCode {
				t.Errorf("Expected status code %d, got %d. Body: %s", tt.expectedStatusCode, rr.Code, rr.Body.String())
			}
			if body := strings.TrimSpace(rr.Body.String()); body != tt.expectedBody {
				t.Errorf("Expected body '%s', got '%s'", tt.expectedBody, body)
			}

			if tt.checkK8sActions != nil {
				tt.checkK8sActions(t, fakeClient.Actions())
			}
		})
	}
}

func TestDetectProxyHandler(t *testing.T) {
	tests := []struct {
		name               string
		method             string
		requestBody        string
		backendHandler     http.HandlerFunc // Mock RayService
		expectedStatusCode int
		expectedBody       string
		expectedHeaders    map[string]string
	}{
		{
			name:        "Success Proxy",
			method:      http.MethodPost,
			requestBody: `{"key": "value"}`,
			backendHandler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("Backend received wrong method: %s", r.Method)
				}
				body, _ := io.ReadAll(r.Body)
				if string(body) != `{"key": "value"}` {
					t.Errorf("Backend received wrong body: %s", string(body))
				}
				w.Header().Set("X-Backend-Header", "backend_value")
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{"result": "ok"}`)
			},
			expectedStatusCode: http.StatusOK,
			expectedBody:       `{"result": "ok"}`,
			expectedHeaders:    map[string]string{"X-Backend-Header": "backend_value"},
		},
		{
			name:        "Error - Wrong Method",
			method:      http.MethodGet,
			requestBody: "",
			backendHandler: func(w http.ResponseWriter, r *http.Request) {
				// This shouldn't be called
				t.Error("Backend handler called unexpectedly")
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectedStatusCode: http.StatusMethodNotAllowed,
			expectedBody:       "Only POST requests are allowed.",
		},
		{
			name:        "Error - Backend Down",
			method:      http.MethodPost,
			requestBody: `{"key": "value"}`,
			backendHandler: func(w http.ResponseWriter, r *http.Request) {
				// This handler won't actually be used if the server isn't started
				panic("Backend should not be reachable for 'Backend Down' test")
			},
			expectedStatusCode: http.StatusBadGateway,
			// Expect a prefix because the error includes OS-specific connection refused details
			expectedBody: "Bad gateway: failed to communicate with detection service at",
		},
		{
			name:        "Error - Backend Returns 500",
			method:      http.MethodPost,
			requestBody: `{"key": "value"}`,
			backendHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, "Backend internal error")
			},
			expectedStatusCode: http.StatusInternalServerError,
			expectedBody:       "Backend internal error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var backendServer *httptest.Server
			var targetURL string

			if tt.name != "Error - Backend Down" {
				// Start the backend server for tests that need it
				backendServer = httptest.NewServer(tt.backendHandler)
				defer backendServer.Close()
				targetURL = backendServer.URL // Use the test server's URL
			} else {
				// For the "Backend Down" test, use a non-existent URL
				// (or we could use the real server's URL but ensure it's stopped)
				targetURL = "http://localhost:9999/nonexistent" // Ensure this port is unlikely to be used
			}

			// Create the handler using the new constructor, passing the target URL
			handler := NewProxyHandler(targetURL)

			reqBody := bytes.NewBufferString(tt.requestBody)
			req, err := http.NewRequest(tt.method, "/detect", reqBody) // The request path to the *proxy* doesn't matter here
			if err != nil {
				t.Fatal(err)
			}
			if tt.method == http.MethodPost {
				req.Header.Set("Content-Type", "application/json")
			}

			rr := httptest.NewRecorder()
			// Use the ServeHTTP method of the ProxyHandler instance
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatusCode {
				t.Errorf("Expected status code %d, got %d. Body: %s", tt.expectedStatusCode, rr.Code, rr.Body.String())
			}

			body := strings.TrimSpace(rr.Body.String())
			// For Bad Gateway, the error message includes specifics about the connection refusal, so we check prefix
			if tt.expectedStatusCode == http.StatusBadGateway {
				if !strings.HasPrefix(body, tt.expectedBody) {
					t.Errorf("Expected body prefix '%s', got '%s'", tt.expectedBody, body)
				}
			} else if body != tt.expectedBody {
				t.Errorf("Expected body '%s', got '%s'", tt.expectedBody, body)
			}

			if tt.expectedHeaders != nil {
				for key, expectedValue := range tt.expectedHeaders {
					if value := rr.Header().Get(key); value != expectedValue {
						t.Errorf("Expected header %s: %s, got: %s", key, expectedValue, value)
					}
				}
			}
		})
	}
}
