package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/craigfurman/herottp"
)

type BoshClient struct {
	boshAdminUsername string
	boshAdminPassword string
	boshURL           string
	httpClient        *herottp.Client
}

func NewBoshClient(boshURL, boshAdminUsername, boshAdminPassword string) *BoshClient {
	return &BoshClient{
		boshURL:           boshURL,
		boshAdminUsername: boshAdminUsername,
		boshAdminPassword: boshAdminPassword,
		httpClient:        herottp.New(herottp.Config{NoFollowRedirect: true, DisableTLSCertificateVerification: true}),
	}
}

func (c *BoshClient) ListDeployments(self string) []string {
	type DeploymentResponse struct {
		Name string `json:"name"`
	}

	req, err := http.NewRequest("GET", fmt.Sprintf("%s/deployments", c.boshURL), nil)
	mustNot(err)
	req.SetBasicAuth(c.boshAdminUsername, c.boshAdminPassword)
	resp, err := c.httpClient.Do(req)
	mustNot(err)

	defer resp.Body.Close()
	respBytes, err := ioutil.ReadAll(resp.Body)
	mustNot(err)
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("expected status 200, got %d. Body: %s\n", resp.StatusCode, string(respBytes))
	}

	var deployments []DeploymentResponse
	must(json.Unmarshal(respBytes, &deployments))

	var deploymentNames []string
	for _, deployment := range deployments {
		if deployment.Name != self {
			deploymentNames = append(deploymentNames, deployment.Name)
		}
	}

	return deploymentNames
}

func (c *BoshClient) DownloadManifest(deploymentName string) string {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/deployments/%s", c.boshURL, deploymentName), nil)
	mustNot(err)
	req.SetBasicAuth(c.boshAdminUsername, c.boshAdminPassword)
	resp, err := c.httpClient.Do(req)
	mustNot(err)

	defer resp.Body.Close()
	respBytes, err := ioutil.ReadAll(resp.Body)
	mustNot(err)
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("expected status 200, got %d. Body: %s\n", resp.StatusCode, string(respBytes))
	}

	var manifestResponse struct {
		Manifest string `json:"manifest"`
	}
	must(json.Unmarshal(respBytes, &manifestResponse))

	return manifestResponse.Manifest
}

func main() {
	log.Println("*** Prepare yourself for scaling")

	port := flag.Int("port", 0, "port")
	boshURL := flag.String("boshUrl", "", "bosh url")
	boshAdminUsername := flag.String("boshAdminUsername", "", "bosh admin username")
	boshAdminPassword := flag.String("boshAdminPassword", "", "bosh admin password")
	thisDeployment := flag.String("thisDeployment", "", "to know bosh first you must know yourself")
	flag.Parse()
	if *port == 0 {
		log.Fatalln("port must be set")
	}
	for _, mandatory := range []string{*boshURL, *boshAdminUsername, *boshAdminPassword, *thisDeployment} {
		if mandatory == "" {
			log.Fatalln("you missed a flag. This is not a useful error message")
		}
	}

	boshClient := NewBoshClient(*boshURL, *boshAdminUsername, *boshAdminPassword)

	for {
		log.Println("=== Beginning scaling loop")

		deployments := boshClient.ListDeployments(*thisDeployment)
		log.Printf("Deployments: %+v\n", deployments)

		for _, deployment := range deployments {
			log.Printf("downloading manifest for deployment: %s\n", deployment)
			manifest := boshClient.DownloadManifest(deployment)
			log.Println(manifest)
		}

		time.Sleep(time.Second * 10)
	}
}

func mustNot(err error) {
	if err != nil {
		log.Fatalf("error: %s\n", err)
	}
}

var must = mustNot
