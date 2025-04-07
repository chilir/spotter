// apps/spotter-manager/cmd/spotter-manager/handlers.go

package main

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"os"
	"text/template"

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
	serviceName := "spotter-svc"
	namespace := "spotter-manager"

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

	serviceName := "spotter-svc"
	namespace := "spotter-manager"

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

// Add other handlers (like List, GetStatus if needed)
