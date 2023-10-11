package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/exec"

	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var clientset *kubernetes.Clientset
var addr string = ""
var host string = ""

func main() {
	// Configure kube API client
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	// Set up http server
	http.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello!\n")
		fmt.Println("/hello endpoint accessed")
	})
	http.HandleFunc("/migrate", createCheckpoint)
	http.HandleFunc("/restore", restore)
	http.HandleFunc("/configure", configure)

	fmt.Printf("Starting server at port 8080\n")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}

// Set destination hostname and destination wormholeserver address
func configure(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("/configure endpoint accessed\n")

	addresses, ok := r.URL.Query()["addr"]
	if !ok || len(addresses[0]) < 1 {
		fmt.Fprintf(w, "Url Param 'addr' is missing\n")
		return
	}
	hosts, ok := r.URL.Query()["host"]
	if !ok || len(hosts[0]) < 1 {
		fmt.Fprintf(w, "Url Param 'host' is missing\n")
		return
	}
	addr = addresses[0]
	host = hosts[0]

	fmt.Fprintf(w, "Destination configured\n")
}

// Migration endpoint handler
func createCheckpoint(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("/checkpoint endpoint accessed\n")

	if addr == "" || host == "" {
		fmt.Fprintf(w, "Destination not configured\n")
		return
	}

	ids, ok := r.URL.Query()["id"]
	if !ok || len(ids[0]) < 1 {
		fmt.Fprintf(w, "Url Param 'id' is missing\n")
		return
	}
	containerId := ids[0]

	// Execute checkpoint helper on host
	cmd := exec.Command("./checkpoint.sh", containerId)
	stdout, err := cmd.Output()
	fmt.Println(string(stdout[:]))
	if err != nil {
		log.Fatal(err.Error())
	}

	pod := getPodObject()

	// Delete source workload
	clientset.CoreV1().Pods(pod.Namespace).Delete(context.TODO(), "workload", metav1.DeleteOptions{})

	_, err = clientset.CoreV1().Pods(pod.Namespace).Create(context.TODO(),
		pod,
		metav1.CreateOptions{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Destination pod created successfully")

	// Wait for destination container to be created
	for pod.Status.Phase != core.PodPending {
		pod, err = clientset.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
		if err != nil {
			log.Fatal(err)
		}
	}
	if pod.Status.Phase != core.PodRunning {
		log.Fatal("Invalid pod status " + pod.Status.Phase)
	}

	// Create destination pod
	id := pod.Status.ContainerStatuses[0].ContainerID
	id = id[13:len(id)] // cut off prefix "containerd://"

	// Restore
	_, err = http.Get(addr + "/restore?id=" + id)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Fprintf(w, "Migration complete\n")
}

// Setup pod definition
func getPodObject() *core.Pod {
	return &core.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "restored",
			Namespace: "default",
		},
		Spec: core.PodSpec{
			Containers: []core.Container{
				{
					Name:            "workload",
					Image:           "nikolabo/alpineio",
					ImagePullPolicy: core.PullIfNotPresent,
				},
			},
			NodeName: host,
		},
	}
}

// Restore endpoint handler
func restore(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("/restore endpoint accessed\n")

	ids, ok := r.URL.Query()["id"]
	if !ok || len(ids[0]) < 1 {
		fmt.Fprintf(w, "Url Param 'id' is missing\n")
		return
	}
	containerId := ids[0]

	// Execute restore helper on host
	cmd := exec.Command("./restore.sh", containerId)
	stdout, err := cmd.Output()
	fmt.Println(string(stdout[:]))
	if err != nil {
		fmt.Println(err.Error())
	}
	fmt.Fprintf(w, "Restore complete\n")
}
