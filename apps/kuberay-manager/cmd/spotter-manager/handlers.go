package main

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
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
		panic(err)
	}
	client, err = dynamic.NewForConfig(config)
	if err != nil {
		panic(err)
	}
}

func ServeFrontend(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "web/index.html")
}

// struct to hold parameters for the template
type RayServiceParams struct {
	ServiceName string
	AppName     string // Optional, defaults to ServiceName in template
	ImportPath  string
	Image       string
	Replicas    int
	Namespace   string
}

func DeployHandler(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	queryParams := r.URL.Query()
	params := RayServiceParams{
		ServiceName: queryParams.Get("serviceName"),
		AppName:     queryParams.Get("appName"), // Optional
		ImportPath:  queryParams.Get("importPath"),
		Image:       queryParams.Get("image"),
		Namespace:   queryParams.Get("namespace"),
	}

	// Validate required parameters
	if params.ServiceName == "" || params.ImportPath == "" || params.Image == "" {
		http.Error(w, "Missing required query parameters: serviceName, importPath, image", http.StatusBadRequest)
		return
	}

	// Parse replicas
	replicasStr := queryParams.Get("replicas")
	if replicasStr == "" {
		replicasStr = "1" // Default to 1 replica if not specified
	}
	var err error
	params.Replicas, err = strconv.Atoi(replicasStr)
	if err != nil || params.Replicas < 0 {
		http.Error(w, fmt.Sprintf("Invalid replicas value: %s. Must be a non-negative integer.", replicasStr), http.StatusBadRequest)
		return
	}

	if params.Namespace == "" {
		params.Namespace = "default"
	}

	// parse the template
	templateBytes, err := os.ReadFile("go/configs/rayservice-template.yaml")
	if err != nil {
		log.Printf("Error reading template file: %v", err)
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

	// Unmarshal YAML to unstructured object
	obj := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(buf.Bytes(), &obj.Object); err != nil {
		log.Printf("Error unmarshalling YAML: %v", err)
		http.Error(w, "Internal server error unmarshalling YAML", http.StatusInternalServerError)
		return
	}

	// Create the resource using the dynamic client
	rayGVR := schema.GroupVersionResource{Group: "ray.io", Version: "v1alpha1", Resource: "rayservices"}

	_, err = client.Resource(rayGVR).Namespace(params.Namespace).Create(r.Context(), obj, metav1.CreateOptions{})
	if err != nil {
		log.Printf("Error creating RayService %s/%s: %v", params.Namespace, params.ServiceName, err)
		http.Error(w, fmt.Sprintf("Failed to create RayService: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated) // Use 201 Created for successful resource creation
	w.Write([]byte(fmt.Sprintf("RayService '%s' created successfully in namespace '%s'", params.ServiceName, params.Namespace)))
}

func DeleteHandler(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	queryParams := r.URL.Query()
	serviceName := queryParams.Get("serviceName")
	namespace := queryParams.Get("namespace")

	// Validate required parameters
	if serviceName == "" {
		http.Error(w, "Missing required query parameter: serviceName", http.StatusBadRequest)
		return
	}
	if namespace == "" {
		namespace = "default"
	}

	rayGVR := schema.GroupVersionResource{Group: "ray.io", Version: "v1alpha1", Resource: "rayservices"}

	err := client.Resource(rayGVR).Namespace(namespace).Delete(r.Context(), serviceName, metav1.DeleteOptions{})
	if err != nil {
		log.Printf("Error deleting RayService %s/%s: %v", namespace, serviceName, err)
		http.Error(w, fmt.Sprintf("Failed to delete RayService: %s", err.Error()), http.StatusInternalServerError)
		return
	}
	w.Write([]byte(fmt.Sprintf("RayService '%s' deleted successfully from namespace '%s'", serviceName, namespace)))
}
