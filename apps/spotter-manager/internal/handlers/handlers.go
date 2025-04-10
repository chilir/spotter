// apps/spotter-manager/internal/handlers.go

package handlers

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"text/template"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

const (
	rayServiceName      = "spotter-ray-service"
	rayServiceNamespace = "spotter"
)

// SetupKubernetesClient initializes and returns a Kubernetes dynamic client.
func SetupKubernetesClient() (dynamic.Interface, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("Failed to get in-cluster config: %w", err)
	}
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("Failed to create dynamic client: %w", err)
	}
	log.Println("Kubernetes dynamic client initialized successfully.")
	return client, nil
}

func ServeFrontend(w http.ResponseWriter, r *http.Request) {
	// prevent caching
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate") // HTTP 1.1
	w.Header().Set("Pragma", "no-cache")                                   // HTTP 1.0
	w.Header().Set("Expires", "0")

	http.ServeFile(w, r, "web/index.html") // relative path to binary in container
}

// MakeDeployHandler creates an HTTP handler for deploying the RayService.
// It captures the dynamic Kubernetes client via closure.
func MakeDeployHandler(client dynamic.Interface) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST requests are allowed.", http.StatusMethodNotAllowed)
			return
		}

		queryParams := r.URL.Query()
		dockerImage := queryParams.Get("dockerimage")
		if dockerImage == "" {
			http.Error(w, "Missing required query parameter: dockerimage", http.StatusBadRequest)
			return
		}
		log.Printf("Attempting to deploy RayService with Docker image: %s", dockerImage)

		params := map[string]string{
			"DockerImage": dockerImage,
		}

		// path to the template relative to the running binary
		templatePath := "configs/rayservice-template.yaml"
		if _, err := os.Stat(templatePath); os.IsNotExist(err) {
			log.Printf("Error: RayService manifest template file not found at %s", templatePath)
			http.Error(
				w,
				"Internal server error: RayService manifest template file missing",
				http.StatusInternalServerError,
			)
			return
		}
		templateBytes, err := os.ReadFile(templatePath)
		if err != nil {
			log.Printf(
				"Error reading RayService manifest template file '%s': %v",
				templatePath,
				err,
			)
			http.Error(
				w,
				"Internal server error reading RayService manifest template",
				http.StatusInternalServerError,
			)
			return
		}
		tmpl, err := template.New("rayservice").Parse(string(templateBytes))
		if err != nil {
			log.Printf("Error parsing RayService manifest template: %v", err)
			http.Error(
				w,
				"Internal server error parsing RayService manifest template",
				http.StatusInternalServerError,
			)
			return
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, params); err != nil {
			log.Printf("Error populating RayService manifest template: %v", err)
			http.Error(
				w,
				"Internal server error populating RayService manifest template",
				http.StatusInternalServerError,
			)
			return
		}

		// log the generated manifest for debugging
		log.Printf("Generated RayService manifest:\n%s", buf.String())

		// k8s yaml decoder
		obj := &unstructured.Unstructured{}
		decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(buf.Bytes()), 4096)
		if err := decoder.Decode(&obj); err != nil {
			log.Printf(
				"Error decoding populated RayService manifest: %v\nYAML:\n%s",
				err,
				buf.String(),
			)
			http.Error(
				w,
				"Internal server error decoding populated RayService manifest",
				http.StatusInternalServerError,
			)
			return
		}
		if obj.Object == nil {
			log.Printf(
				"Error: decoded RayService manifest is nil. Check template output.\nYAML:\n%s",
				buf.String(),
			)
			http.Error(
				w,
				"Internal server error: failed to parse decoded populated RayService manifest",
				http.StatusInternalServerError,
			)
			return
		}

		// Define the GroupVersionResource for RayService
		rayGVR := schema.GroupVersionResource{
			Group:    "ray.io",
			Version:  "v1alpha1",
			Resource: "rayservices",
		}

		log.Printf(
			"Applying RayService configuration %s/%s...",
			rayServiceNamespace,
			rayServiceName,
		)

		applyOptions := metav1.ApplyOptions{
			FieldManager: "spotter-manager",
			Force:        true,
		}
		appliedObj, err := client.Resource(rayGVR).Namespace(rayServiceNamespace).Apply(
			r.Context(),
			rayServiceName,
			obj,
			applyOptions,
		)

		if err != nil {
			log.Printf(
				"Error applying RayService %s/%s: %v",
				rayServiceNamespace,
				rayServiceName,
				err,
			)
			http.Error(
				w,
				fmt.Sprintf(
					"Internal server error: failed to apply RayService '%s' in namespace '%s': %s",
					rayServiceName,
					rayServiceNamespace,
					err.Error(),
				),
				http.StatusInternalServerError,
			)
			return
		}

		log.Printf(
			"Successfully applied RayService configuration %s/%s (UID: %s)",
			rayServiceNamespace,
			rayServiceName,
			appliedObj.GetUID(), // Use the object returned by Apply
		)
		w.WriteHeader(http.StatusOK) // 200
		fmt.Fprintf(
			w,
			"RayService '%s' applied successfully in namespace '%s'",
			rayServiceName,
			rayServiceNamespace,
		)
	}
}

// MakeDeleteHandler creates an HTTP handler for deleting the RayService.
// It captures the dynamic Kubernetes client via closure.
func MakeDeleteHandler(client dynamic.Interface) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST requests are allowed.", http.StatusMethodNotAllowed)
			return
		}

		log.Printf("Attempting to delete RayService %s/%s", rayServiceNamespace, rayServiceName)

		rayGVR := schema.GroupVersionResource{Group: "ray.io", Version: "v1alpha1", Resource: "rayservices"}

		err := client.Resource(rayGVR).Namespace(rayServiceNamespace).Delete(r.Context(), rayServiceName, metav1.DeleteOptions{})
		if err != nil {
			// Check if the error is 'NotFound'
			if errors.IsNotFound(err) {
				log.Printf("RayService %s/%s not found, considering deletion successful (idempotent).", rayServiceNamespace, rayServiceName)
				// Still return success as the desired state (not existing) is achieved
			} else {
				// Handle other errors as internal server errors
				log.Printf("Error deleting RayService %s/%s: %v", rayServiceNamespace, rayServiceName, err)
				http.Error(
					w,
					fmt.Sprintf(
						"Internal server error: failed to delete RayService '%s' in namespace '%s': %s",
						rayServiceName,
						rayServiceNamespace,
						err.Error(),
					),
					http.StatusInternalServerError,
				)
				return // Important: return after sending error
			}
		}

		if err == nil {
			log.Printf(
				"Successfully initiated deletion for RayService %s/%s",
				rayServiceNamespace,
				rayServiceName,
			)
		}
		// Send success response whether deletion happened or resource was already gone
		w.WriteHeader(http.StatusOK) // Ensure status is OK for idempotency

		// Customize message based on whether the resource existed
		var responseMsg string
		if err != nil && errors.IsNotFound(err) {
			// It was a NotFound error
			responseMsg = fmt.Sprintf("RayService '%s' in namespace '%s' did not exist.", rayServiceName, rayServiceNamespace)
		} else {
			// Either err was nil (successful delete) or it was some other non-NotFound error handled above
			// We only reach here for err == nil in practice because other errors return earlier.
			responseMsg = fmt.Sprintf("RayService '%s' deleted successfully from namespace '%s'", rayServiceName, rayServiceNamespace)
		}
		fmt.Fprint(w, responseMsg) // Use Fprint as we don't need formatting here
	}
}

// ProxyHandler holds dependencies for the detect proxy
type ProxyHandler struct {
	TargetURL string
	Client    *http.Client // Allow injecting a client for testing/timeouts
}

// NewProxyHandler creates a new ProxyHandler
// If targetURLOverride is empty, it constructs the default RayService URL.
func NewProxyHandler(targetURLOverride string) *ProxyHandler {
	targetURL := targetURLOverride
	if targetURL == "" {
		// Construct the default URL if no override is provided
		targetURL = fmt.Sprintf(
			"http://%s-head-svc.%s.svc.cluster.local:8000/detect",
			rayServiceName,
			rayServiceNamespace,
		)
	}
	return &ProxyHandler{
		TargetURL: targetURL,
		Client: &http.Client{
			Timeout: 60 * time.Second, // Keep the default timeout
		},
	}
}

// ServeHTTP forwards requests to the configured TargetURL
func (h *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST requests are allowed.", http.StatusMethodNotAllowed)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		http.Error(w, "Error reading request", http.StatusBadRequest)
		return
	}
	r.Body.Close() // Close the original request body

	// Use h.TargetURL instead of the hardcoded one
	proxyReq, err := http.NewRequestWithContext(
		r.Context(),
		"POST",
		h.TargetURL,
		bytes.NewReader(bodyBytes), // Create a new reader for the proxy request
	)
	if err != nil {
		log.Printf("Error creating proxy request: %v", err)
		http.Error(w, "Internal server error creating proxy request", http.StatusInternalServerError)
		return
	}
	// Copy headers from the original request
	proxyReq.Header = r.Header.Clone()

	// Use the client from the handler struct
	resp, err := h.Client.Do(proxyReq)
	if err != nil {
		log.Printf("Error forwarding request to target %s: %v", h.TargetURL, err)
		http.Error(
			w,
			fmt.Sprintf(
				"Bad gateway: failed to communicate with detection service at %s: %v",
				h.TargetURL,
				err,
			),
			http.StatusBadGateway,
		)
		return
	}
	defer resp.Body.Close()

	// Copy headers from the backend response to the original response writer
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Copy the status code from the backend response
	w.WriteHeader(resp.StatusCode)

	// Copy the body from the backend response
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("Error copying response body from target %s: %v", h.TargetURL, err)
		// Don't write another error header if one has already been written
		if !headerWritten(w) {
			http.Error(w, "Internal server error reading backend response", http.StatusInternalServerError)
		}
		return
	}

	log.Printf("Successfully proxied detection request to %s", h.TargetURL)
}

// Helper function to check if the response header has been written
// This prevents writing multiple headers, which causes warnings.
func headerWritten(w http.ResponseWriter) bool {
	// Accessing the underlying ResponseWriter to check the status code.
	// This is a common pattern but relies on the implementation detail.
	// A more robust way might involve custom ResponseWriter wrappers.
	if ww, ok := w.(interface{ Status() int }); ok {
		return ww.Status() != 0
	}
	// Fallback check: Check if headers have been written via WriteHeader call implicitly
	// This is less reliable as Write() also writes headers.
	// A simple heuristic: check if a common header exists.
	return w.Header().Get("Date") != ""
}
