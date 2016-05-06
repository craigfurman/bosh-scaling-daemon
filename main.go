package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/craigfurman/herottp"
	"github.com/garyburd/redigo/redis"
	"gopkg.in/yaml.v2"
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

type DeploymentResponse struct {
	Name string `json:"name"`
}

func (c *BoshClient) ListDeployments(self string) []string {
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

type TaskResponse struct {
	State string `json:"state"`
}

func (c *BoshClient) GetTaskState(taskID string) string {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/tasks/%s", c.boshURL, taskID), nil)
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

	var taskResponse TaskResponse
	must(yaml.Unmarshal(respBytes, &taskResponse))
	return taskResponse.State
}

func (c *BoshClient) Deploy(manifest Manifest) {
	manifestBytes, err := yaml.Marshal(manifest)
	mustNot(err)
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/deployments", c.boshURL), bytes.NewReader(manifestBytes))
	mustNot(err)
	req.Header.Set("Content-Type", "text/yaml")
	req.SetBasicAuth(c.boshAdminUsername, c.boshAdminPassword)
	c.PerformAsyncReqestAndAwaitEndOfTask(req)
}

func (c *BoshClient) PerformAsyncReqestAndAwaitEndOfTask(req *http.Request) string {
	resp, err := c.httpClient.Do(req)
	mustNot(err)

	if resp.StatusCode != http.StatusFound {
		defer resp.Body.Close()
		respBytes, err := ioutil.ReadAll(resp.Body)
		mustNot(err)
		log.Fatalf("expected status 302, got %d. Body: %s\n", resp.StatusCode, string(respBytes))
	}

	locationComponents := strings.Split(resp.Header.Get("Location"), "/")
	boshTaskID := locationComponents[len(locationComponents)-1]
	log.Printf("following bosh task %s\n", boshTaskID)
	for {
		taskState := c.GetTaskState(boshTaskID)
		log.Printf("task state is %s\n", taskState)
		time.Sleep(time.Second * 10)
		switch taskState {
		case "done":
			return boshTaskID
		case "error":
			log.Fatalln("a task failed, I don't know what to do with myself!")
		default:
			continue
		}
	}
}

type InstanceGroupResponse struct {
	Name string   `json:"job_name"`
	IPs  []string `json:"ips"`
}

func (c *BoshClient) GetInstanceHosts(deployment string) map[string][]string {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/deployments/%s/vms?format=full", c.boshURL, deployment), nil)
	mustNot(err)
	req.SetBasicAuth(c.boshAdminUsername, c.boshAdminPassword)
	taskID := c.PerformAsyncReqestAndAwaitEndOfTask(req)

	req, err = http.NewRequest("GET", fmt.Sprintf("%s/tasks/%s/output?type=result", c.boshURL, taskID), nil)
	mustNot(err)
	req.SetBasicAuth(c.boshAdminUsername, c.boshAdminPassword)
	resp, err := c.httpClient.Do(req)
	mustNot(err)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBytes, err := ioutil.ReadAll(resp.Body)
		mustNot(err)
		log.Fatalf("expected status 200, got %d. Body: %s\n", resp.StatusCode, string(respBytes))
	}

	hosts := map[string][]string{}
	decoder := json.NewDecoder(resp.Body)

	for decoder.More() {
		var instanceGroupResp InstanceGroupResponse
		must(decoder.Decode(&instanceGroupResp))
		hosts[instanceGroupResp.Name] = append(hosts[instanceGroupResp.Name], instanceGroupResp.IPs...)
	}
	return hosts
}

func main() {
	log.Println("*** Prepare yourself for scaling")

	port := flag.Int("port", 0, "port")
	boshURL := flag.String("boshUrl", "", "bosh url")
	boshAdminUsername := flag.String("boshAdminUsername", "", "bosh admin username")
	boshAdminPassword := flag.String("boshAdminPassword", "", "bosh admin password")
	thisDeployment := flag.String("thisDeployment", "", "to know bosh first you must know yourself")
	maxInstanceCount := 4          // TODO make configurable
	redisMemoryCutoff := 2000000.0 // TODO make configurable
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
			manifestBytes := boshClient.DownloadManifest(deployment)
			log.Println(manifestBytes)

			var manifest Manifest
			must(yaml.Unmarshal([]byte(manifestBytes), &manifest))

			hosts := boshClient.GetInstanceHosts(deployment)
			log.Printf("bosh vms: %+v\n", hosts)

			scaledDeployment := false
			for _, instanceGroup := range manifest.InstanceGroups {
				if instanceGroup.Instances >= maxInstanceCount {
					log.Printf("instance group %s has count %d greater than the max (%d). Will not scale, ever\n", instanceGroup.Name, instanceGroup.Instances, maxInstanceCount)
					continue
				}

				// TODO use non-redis-specific criteria for scaling up. This assumes every instance is a Redis!
				totalMemory := 0.0
				for _, host := range hosts[instanceGroup.Name] {
					// TODO the password isn't always "password"
					conn, err := redis.Dial("tcp", fmt.Sprintf("%s:6379", host), redis.DialPassword("password"))
					mustNot(err)
					memoryOutput, err := redis.String(conn.Do("INFO", "memory"))
					mustNot(err)
					conn.Close()
					for _, line := range strings.Split(memoryOutput, "\n") {
						if strings.Contains(line, "used_memory:") {
							nodeMemory, err := strconv.Atoi(strings.Split(strings.TrimSpace(line), ":")[1])
							mustNot(err)
							log.Printf("memory used by %s: %d\n", host, nodeMemory)
							totalMemory += float64(nodeMemory)
						}
					}
				}
				avgMem := totalMemory / float64(len(hosts[instanceGroup.Name]))
				fmt.Printf("average memory used per node: %f\n", avgMem)

				if avgMem > redisMemoryCutoff {
					scaledDeployment = true
					newInstanceCount := 2 * instanceGroup.Instances
					log.Printf("will scale instance group %s to %d instances\n", instanceGroup.Name, newInstanceCount)
					instanceGroup.Instances = newInstanceCount
				}
			}

			if scaledDeployment {
				log.Printf("scaling with new manifest: %+v\n", manifest)
				boshClient.Deploy(manifest)
			}
		}

		time.Sleep(time.Second * 10)
	}
}

// This is missing a LOT of optional manifest fields
type Manifest struct {
	Name         string `yaml:"name"`
	DirectorUUID string `yaml:"director_uuid"`
	Releases     []struct {
		Name    string
		Version string
	}
	Stemcells []struct {
		Alias   string
		OS      string
		Version string
	}
	InstanceGroups []*struct {
		Name      string `yaml:"name"`
		Instances int    `yaml:"instances"`
		Jobs      []struct {
			Name    string
			Release string
		}
		VMType   string `yaml:"vm_type"`
		Stemcell string
		Networks []struct {
			Name string
		}
	} `yaml:"instance_groups"`
	Properties map[interface{}]interface{}
	Update     struct {
		Canaries        int
		CanaryWatchTime string `yaml:"canary_watch_time"`
		UpdateWatchTime string `yaml:"update_watch_time"`
		MaxInFlight     int    `yaml:"max_in_flight"`
	}
}

func mustNot(err error) {
	if err != nil {
		log.Fatalf("error: %s\n", err)
	}
}

var must = mustNot
