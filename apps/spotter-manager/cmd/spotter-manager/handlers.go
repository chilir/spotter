// apps/spotter-manager/cmd/spotter-manager/handlers.go

package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"text/template"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

var client dynamic.Interface

func init() {
	config, err := rest.InClusterConfig()
	if err != nil {
		// Consider more robust error handling or logging for production
		log.Fatalf("Failed to get in-cluster config: %v", err)
	}
	client, err = dynamic.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create dynamic client: %v", err)
	}
}

func ServeFrontend(w http.ResponseWriter, r *http.Request) {
	// Add cache control headers to prevent caching of index.html
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate") // HTTP 1.1.
	w.Header().Set("Pragma", "no-cache")                                   // HTTP 1.0.
	w.Header().Set("Expires", "0")                                         // Proxies.

	http.ServeFile(w, r, "web/index.html")
}

func DeployHandler(w http.ResponseWriter, r *http.Request) {
	// Check if the request method is POST
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed. Use POST.", http.StatusMethodNotAllowed)
		return
	}

	// Parse query parameters
	queryParams := r.URL.Query()
	dockerImage := queryParams.Get("image")

	// Validate required parameter
	if dockerImage == "" {
		http.Error(w, "Missing required query parameter: image", http.StatusBadRequest)
		return
	}

	// Optional: Log the parameters being used
	log.Printf("Attempting to deploy RayService with image: %s", dockerImage)

	// Define parameters for template - only image is used
	params := map[string]string{
		"Image": dockerImage,
	}

	// path to the template relative to the running binary
	templatePath := "configs/rayservice-template.yaml"

	// Check if template file exists
	if _, err := os.Stat(templatePath); os.IsNotExist(err) {
		log.Printf("Error: Template file not found at %s", templatePath)
		http.Error(w, "Internal server error: Template file missing", http.StatusInternalServerError)
		return
	}

	// Parse the template
	templateBytes, err := os.ReadFile(templatePath)
	if err != nil {
		log.Printf("Error reading template file '%s': %v", templatePath, err)
		http.Error(w, "Internal server error reading template", http.StatusInternalServerError)
		return
	}

	tmpl, err := template.New("rayservice").Parse(string(templateBytes))
	if err != nil {
		log.Printf("Error parsing template: %v", err)
		http.Error(w, "Internal server error parsing template", http.StatusInternalServerError)
		return
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, params); err != nil {
		log.Printf("Error executing template: %v", err)
		http.Error(w, "Internal server error executing template", http.StatusInternalServerError)
		return
	}

	// Optional: Log the generated manifest for debugging
	// log.Printf("Generated RayService Manifest:\n%s", buf.String())

	// Unmarshal YAML to unstructured object
	obj := &unstructured.Unstructured{}
	// Use Kubernetes YAML decoder which handles multi-document files (though template should be single)
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(buf.Bytes()), 4096)
	if err := decoder.Decode(&obj); err != nil {
		log.Printf("Error decoding generated YAML: %v\nYAML:\n%s", err, buf.String())
		http.Error(w, "Internal server error decoding generated YAML", http.StatusInternalServerError)
		return
	}

	// Check if decoding produced an empty object
	if obj.Object == nil {
		log.Printf("Error: Decoded object is nil. Check template output.\nYAML:\n%s", buf.String())
		http.Error(w, "Internal server error: Failed to parse generated manifest", http.StatusInternalServerError)
		return
	}

	// Define the GroupVersionResource for RayService
	rayGVR := schema.GroupVersionResource{Group: "ray.io", Version: "v1alpha1", Resource: "rayservices"}

	// Create the resource using the dynamic client in the hardcoded namespace
	serviceName := "spotter-ray-service"
	namespace := "spotter"

	log.Printf("Creating RayService resource %s/%s...", namespace, serviceName)
	createdObj, err := client.Resource(rayGVR).Namespace(namespace).Create(r.Context(), obj, metav1.CreateOptions{})
	if err != nil {
		log.Printf("Error creating RayService %s/%s: %v", namespace, serviceName, err)
		// Provide more context in the error message if possible
		http.Error(w, fmt.Sprintf("Failed to create RayService '%s' in namespace '%s': %s", serviceName, namespace, err.Error()), http.StatusInternalServerError)
		return
	}

	log.Printf("Successfully created RayService %s/%s (UID: %s)", namespace, serviceName, createdObj.GetUID())
	w.WriteHeader(http.StatusCreated) // Use 201 Created for successful resource creation
	fmt.Fprintf(w, "RayService '%s' created successfully in namespace '%s'", serviceName, namespace)
}

func DeleteHandler(w http.ResponseWriter, r *http.Request) {
	// Check if the request method is POST
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed. Use POST.", http.StatusMethodNotAllowed)
		return
	}

	serviceName := "spotter-ray-service"
	namespace := "spotter"

	log.Printf("Attempting to delete RayService %s/%s", namespace, serviceName)

	rayGVR := schema.GroupVersionResource{Group: "ray.io", Version: "v1alpha1", Resource: "rayservices"}

	err := client.Resource(rayGVR).Namespace(namespace).Delete(r.Context(), serviceName, metav1.DeleteOptions{})
	if err != nil {
		// Check if the error is "not found" vs. other errors
		// if errors.IsNotFound(err) { ... }
		log.Printf("Error deleting RayService %s/%s: %v", namespace, serviceName, err)
		http.Error(w, fmt.Sprintf("Failed to delete RayService '%s' in namespace '%s': %s", serviceName, namespace, err.Error()), http.StatusInternalServerError)
		return
	}

	log.Printf("Successfully initiated deletion for RayService %s/%s", namespace, serviceName)
	fmt.Fprintf(w, "RayService '%s' deleted successfully from namespace '%s'", serviceName, namespace)
}

// DetectProxyHandler forwards requests to the RayService endpoint for detection
func DetectProxyHandler(w http.ResponseWriter, r *http.Request) {
	serviceName := "spotter-ray-service"
	namespace := "spotter"

	// Only accept POST method
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed. Use POST.", http.StatusMethodNotAllowed)
		return
	}

	// KubeRay creates a service for Ray Serve with this naming pattern
	// The route_prefix in the template is set to /detect
	// Use the head service name convention, which exposes port 8000 for serve
	rayServiceURL := fmt.Sprintf("http://%s-head-svc.%s.svc.cluster.local:8000/detect",
		serviceName, namespace)

	// Read the request body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		http.Error(w, "Error reading request", http.StatusBadRequest)
		return
	}
	r.Body.Close()

	// Create a new request to the Ray service
	proxyReq, err := http.NewRequestWithContext(r.Context(), "POST", rayServiceURL, bytes.NewReader(bodyBytes))
	if err != nil {
		log.Printf("Error creating proxy request: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Copy headers from the original request
	proxyReq.Header = r.Header.Clone()

	// Make the request to the Ray service
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(proxyReq)
	if err != nil {
		log.Printf("Error forwarding request to Ray service: %v", err)
		http.Error(w, fmt.Sprintf("Error communicating with detection service: %v", err),
			http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Read the response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response from Ray service: %v", err)
		http.Error(w, "Error reading response from detection service",
			http.StatusInternalServerError)
		return
	}

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Set the status code and write the response body
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)

	log.Printf("Successfully proxied detection request to RayService %s", rayServiceURL)
}

// Add other handlers (like List, GetStatus if needed)
