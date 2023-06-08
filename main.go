package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	"github.com/kubernetes-client/go/kubernetes/config/api"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	namespace  = flag.String("namespace", "", "namespace of deployment")
	prefix     = flag.String("prefix", "", "prefix filter for clusters")
)

func main() {
	flag.Parse()
	var err error

	var config *rest.Config
	if *kubeconfig == "" {
		config, err = rest.InClusterConfig()
		if err != nil {
			log.Fatal(err.Error())
		}
	} else {
		config, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
	}

	if err != nil {
		log.Fatal(err.Error())
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err.Error())
	}

	watcher, err := clientset.CoreV1().Secrets(*namespace).Watch(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.Fatal(err)
	}

	for event := range watcher.ResultChan() {
		sec := event.Object.(*v1.Secret)
		if strings.HasPrefix(sec.Name, *prefix) && strings.Contains(sec.Name, "-kubeconfig") {
			cluName := strings.ReplaceAll(sec.Name, "-kubeconfig", "")
			switch event.Type {
			case watch.Added, watch.Modified:
				fmt.Printf("Detected new kubeconfig secret %s\n", sec.Name)
				kc := api.Config{}
				err = yaml.Unmarshal(sec.Data["value"], &kc)
				if err != nil {
					fmt.Printf("failed to parse kubeconfg err:%v\n", err.Error())
					continue
				}
				kcBytes, _ := yaml.Marshal(kc)
				err := os.WriteFile(cluName, kcBytes, 0644)
				if err != nil {
					fmt.Printf("failed to create kubeconfig file for cluster '%s': %v\n", cluName, err)
					continue
				}
				registerToEKS(cluName)
			case watch.Deleted:
				fmt.Printf("Detected deleted kubeconfig secret %s\n", sec.Name)
				deregisterFromEKS(cluName)
			}
		}
	}

}

func registerToEKS(clusterName string) {
	provider := os.Getenv("CLUSTER_PROVIDER")
	if provider == "" {
		provider = "EKS_ANYWHERE"
	}
	region := os.Getenv("AWS_DEFAULT_REGION")
	role := os.Getenv("AWS_EKS_CONNECTOR_ROLE_ARN")

	cmd := exec.Command("eksctl", "get", "cluster", "--name", clusterName, "--region", region)
	_, err := cmd.Output()
	if err == nil {
		fmt.Printf("cluster '%s' already registered\n", clusterName)
		return
	}

	cmd = exec.Command("eksctl", "register", "cluster", "--name", clusterName, "--provider", provider, "--region", region, "--role-arn", role)
	_, err = cmd.Output()
	if err != nil {
		fmt.Printf("failed to register cluster '%s': %v\n", clusterName, err)
	}

	retries := 1
	for {
		cmd = exec.Command("kubectl", "--kubeconfig", clusterName, "apply", "-f", "eks-connector.yaml,eks-connector-clusterrole.yaml,eks-connector-console-dashboard-full-access-group.yaml")
		_, err = cmd.Output()
		if err != nil {
			fmt.Printf("failed to apply registration manifests in cluster '%s': %v, Retrying...\n", clusterName, err)
			time.Sleep(3 * time.Second)
			retries += 1
			if retries > 3 {
				fmt.Printf("failed to apply registration manifests in cluster '%s': %v, Aborting...\n", clusterName, err)
				return
			}
			continue
		}
		break
	}
	fmt.Printf("cluster '%s' registered successfully\n", clusterName)
}

func deregisterFromEKS(clusterName string) {
	region := os.Getenv("AWS_DEFAULT_REGION")

	cmd := exec.Command("eksctl", "get", "cluster", "--name", clusterName, "--region", region)
	_, err := cmd.Output()
	if err != nil && strings.Contains(err.Error(), "404") {
		fmt.Printf("cluster '%s' already deregistered\n", clusterName)
		return
	}

	cmd = exec.Command("eksctl", "deregister", "cluster", "--name", clusterName, "--region", region)
	_, err = cmd.Output()
	if err != nil {
		fmt.Printf("failed to deregister cluster '%s': %v\n", clusterName, err)
	}
	fmt.Printf("cluster '%s' deregistered successfully\n", clusterName)
}
