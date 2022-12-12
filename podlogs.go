/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Note: the example only works with the code within the same release/branch.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	//
	// Uncomment to load all auth plugins
	// _ "k8s.io/client-go/plugin/pkg/client/auth"
	//
	// Or uncomment to load specific auth plugins
	// _ "k8s.io/client-go/plugin/pkg/client/auth/azure"
	// _ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
)

type S3Provider struct {
	sess       *session.Session
	uploader   *s3manager.Uploader
	bucketName string
}

type ServiceLogs struct {
	ServiceName   string    `json:"serviceName`
	ClusterName   string    `json:"clusterName"`
	PodName       string    `json:"podName"`
	ContainerName string    `json:"containerName"`
	LastTimestamp time.Time `json:"lastTimestamp"`
	Logs          string    `json:"logs"`
}

func buildConfigFromFlags(context, kubeconfigPath string) (*rest.Config, error) {
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		&clientcmd.ConfigOverrides{
			CurrentContext: context,
		}).ClientConfig()
}

var kubeconfig string

func init() {
	home := homedir.HomeDir()
	kubeconfig = filepath.Join(home, ".kube", "config")
}

func main() {
	router := gin.Default()
	router.GET("/api/logs", getLogs)

	router.Run("localhost:5000")

}

func getLogs(c *gin.Context) {
	serviceName := c.Query("serviceName")
	podName := c.Query("podName")
	containerName := c.Query("containerName")
	clusterName := c.Query("clusterName")
	sinceTimeStr := c.Query("sinceTime")
	var sinceTime metav1.Time
	if sinceTimeStr != "" {
		newTime, _ := time.Parse(time.RFC3339, sinceTimeStr)
		log.Print(newTime)
		sinceTime = metav1.NewTime(newTime)
	}
	currentTime := time.Now().UTC()
	log.Print(currentTime)
	clientConfig, err := buildConfigFromFlags(clusterName, kubeconfig)
	if err != nil {
		fmt.Print(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	clientset, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		panic(err.Error())
	}
	podClient := clientset.CoreV1().Pods(serviceName)
	/*podList, err := podClient.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		log.Print(err.Error())
		return
	}
	for _, item := range podList.Items {
		log.Print(item.Name)
	}*/
	podLogOptions := &corev1.PodLogOptions{
		Container: containerName,
		//Timestamps: true,
	}
	if sinceTimeStr != "" {
		podLogOptions.SinceTime = &sinceTime
	} else {
		var defaultTailLines int64 = 50
		podLogOptions.TailLines = &defaultTailLines
	}
	req := podClient.GetLogs(podName, podLogOptions)
	log.Print("Fetching logs")
	readCloser, err := req.Stream(context.Background())
	if err != nil {
		msg := fmt.Sprintf("Failed to get logs due to %s", err)
		log.Print(msg)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer readCloser.Close()
	message := ""
	for {
		buf := make([]byte, 2000)
		numBytes, err := readCloser.Read(buf)
		if err == io.EOF {
			log.Print("end of stream")
			break
		}
		if numBytes == 0 {
			log.Print("no data")
			continue
		}
		if err != nil {
			fmt.Print(err)
			log.Print("encountered error")
		}
		//log.Printf("Number of Bytes: %d", len(buf[:numBytes]))
		msg := string(buf[:numBytes])
		message = message + msg
		//log.Print(message)
	}
	serviceLogs := ServiceLogs{
		ClusterName:   clusterName,
		ServiceName:   serviceName,
		PodName:       podName,
		Logs:          message,
		ContainerName: containerName,
		LastTimestamp: currentTime,
	}
	c.IndentedJSON(http.StatusOK, serviceLogs)
}
