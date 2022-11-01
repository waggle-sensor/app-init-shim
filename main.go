package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/go-redis/redis/v9"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func main() {
	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Fatalf("failed to get config: %s", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("failed to create clientset: %s", err)
	}

	ctx, cancel := context.WithTimeout(context.TODO(), 10*time.Second)
	defer cancel()

	appID := os.Getenv("WAGGLE_APP_ID")
	if appID != "" {
		log.Fatalf("WAGGLE_APP_ID is required.")
	}

	meta := map[string]string{}

	// add fields from env vars
	env2meta := map[string]string{
		"HOST":   "host",
		"JOB":    "job",
		"TASK":   "task",
		"PLUGIN": "plugin",
	}

	for envKey, metaKey := range env2meta {
		if value, ok := os.LookupEnv(envKey); ok && value != "" {
			meta[metaKey] = value
		} else {
			log.Fatalf("Env var %s is required and must be nonempty.", envKey)
		}
	}

	// Now, since we're able to add a little more logic than in the current init container, we can
	// do things like look up additional node labels and metadata for tagging.
	node, err := clientset.CoreV1().Nodes().Get(ctx, meta["host"], v1.GetOptions{})
	if err != nil {
		log.Fatalf("failed to get node: %s", err)
	}

	// NOTE(sean) I think zone may actually be used as meta for some sys metrics. may need to choose a different name.
	if zone, ok := node.ObjectMeta.Labels["zone"]; ok {
		meta["zone"] = zone
	}

	log.Printf("meta: %v", meta)

	// set meta in redis meta cache
	// ref: https://github.com/waggle-sensor/edge-scheduler/blob/d6dc256b6777fdefc94ee8c45a403d075ef194ae/pkg/nodescheduler/resourcemanager.go#L481
	rdb := redis.NewClient(&redis.Options{
		Addr: "wes-app-meta-cache:6379",
	})

	b, err := json.Marshal(meta)
	if err != nil {
		log.Fatalf("failed to create app meta json: %s", err)
	}

	if err := rdb.Set(ctx, fmt.Sprintf("app-meta.%s", appID), string(b), 0); err != nil {
		log.Fatalf("failed to set app meta cache data: %s", err)
	}
}
