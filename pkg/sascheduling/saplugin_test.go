package sascheduling

import (
	"context"

	"encoding/json"
	"fmt"

	"k8s.io/client-go/informers"
	clientsetfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/events"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/defaultbinder"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/queuesort"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"
	tf "k8s.io/kubernetes/pkg/scheduler/testing/framework"
	testutil "sigs.k8s.io/scheduler-plugins/test/util"
	"sort"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

type TestParameters struct {
	NumNodes       int
	NumRegions     int
	NodesPerRegion int
	NumPods        int
	PodCPU         int64
	PodMemory      int64
}

var testCases = []TestParameters{
	{
		NumNodes:       9,
		NumRegions:     3,
		NodesPerRegion: 3,
		NumPods:        10,
		PodCPU:         1000,
		PodMemory:      1024,
	},
	{
		NumNodes:       9,
		NumRegions:     3,
		NodesPerRegion: 3,
		NumPods:        12,
		PodCPU:         1000,
		PodMemory:      1024,
	},
	{
		NumNodes:       12,
		NumRegions:     3,
		NodesPerRegion: 3,
		NumPods:        15,
		PodCPU:         1000,
		PodMemory:      1024,
	},
	{
		NumNodes:       12,
		NumRegions:     3,
		NodesPerRegion: 3,
		NumPods:        20,
		PodCPU:         1000,
		PodMemory:      1024,
	},
	{
		NumNodes:       20,
		NumRegions:     3,
		NodesPerRegion: 5,
		NumPods:        25,
		PodCPU:         1000,
		PodMemory:      1024,
	},
	{
		NumNodes:       20,
		NumRegions:     3,
		NodesPerRegion: 5,
		NumPods:        30,
		PodCPU:         1000,
		PodMemory:      1024,
	},
	{
		NumNodes:       20,
		NumRegions:     3,
		NodesPerRegion: 5,
		NumPods:        35,
		PodCPU:         1000,
		PodMemory:      1024,
	},
	{
		NumNodes:       20,
		NumRegions:     3,
		NodesPerRegion: 5,
		NumPods:        40,
		PodCPU:         1000,
		PodMemory:      1024,
	},
}

var RegionLatencyMap = map[string]map[string]float64{
	"region1": {
		"region1": 0.7, // intra-region latency
		"region2": 3.0,
		"region3": 5.0,
	},
	"region2": {
		"region1": 3.0,
		"region2": 0.7, // intra-region latency
		"region3": 5.0,
	},
	"region3": {
		"region1": 10,
		"region2": 5,
		"region3": 0.7, // intra-region latency

	},
}

func TestSAPluginWithParameters(t *testing.T) {

	for _, params := range testCases {
		t.Logf("Running test with parameters: Nodes=%d, Regions=%d, Pods=%d",
			params.NumNodes, params.NumRegions, params.NumPods)
		runTestCase(t, params)

	}
}

func runTestCase(t *testing.T, params TestParameters) {

	var allNodes []*v1.Node
	for r := 1; r <= params.NumRegions; r++ {
		regionNodes := createNodesInRegion(
			fmt.Sprintf("region%d", r),
			(r-1)*params.NodesPerRegion+1,
			params.NodesPerRegion,
			4000,
			8192,
		)
		allNodes = append(allNodes, regionNodes...)
	}

	nodeInfoMap := make(map[string]*framework.NodeInfo)
	for _, node := range allNodes {
		nodeInfo := framework.NewNodeInfo()
		nodeInfo.SetNode(node)
		nodeInfoMap[node.Name] = nodeInfo
	}

	var pods []*v1.Pod
	for i := 1; i <= params.NumPods; i++ {
		pods = append(pods, mockPod(
			fmt.Sprintf("pod%d", i),
			params.PodCPU,
			params.PodMemory,
			"test-group",
		))
	}

	client := clientsetfake.NewSimpleClientset()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, pod := range pods {
		_, err := client.CoreV1().Pods("default").Create(context.Background(), pod, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Failed to create pod: %v", err)
		}
	}

	for _, node := range allNodes {
		_, err := client.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Failed to create node: %v", err)
		}
	}

	var registerPlugins []tf.RegisterPluginFunc
	registeredPlugins := append(
		registerPlugins,
		tf.RegisterQueueSortPlugin(queuesort.Name, queuesort.New),
		tf.RegisterBindPlugin(defaultbinder.Name, defaultbinder.New),
	)

	informerFactory := informers.NewSharedInformerFactory(client, 0)
	nodeInformer := informerFactory.Core().V1().Nodes().Informer()
	podInformer := informerFactory.Core().V1().Pods().Informer()
	informerFactory.Start(nil)
	cache.WaitForCacheSync(nil, nodeInformer.HasSynced, podInformer.HasSynced)

	mockHandle, err := tf.NewFramework(
		ctx,
		registeredPlugins,
		"default-scheduler",
		frameworkruntime.WithClientSet(client),
		frameworkruntime.WithEventRecorder(&events.FakeRecorder{}),
		frameworkruntime.WithInformerFactory(informerFactory),
		frameworkruntime.WithPodNominator(testutil.NewPodNominator(informerFactory.Core().V1().Pods().Lister())),
		frameworkruntime.WithSnapshotSharedLister(newFakeSharedLister(pods, nodeInfoMap)),
	)

	if err != nil {
		t.Fatal(err)
	}

	createLatencyConfigMaps(client)
	plugin := &SAPlugin{
		handle:      mockHandle,
		latencyData: make(map[string]map[string]float64),
		groupPods:   make(map[string][]*v1.Pod),
	}

	state := framework.NewCycleState()

	nodeScores := make(map[string]map[string]int64)
	var allLatencies []float64
	totalLatency := 0.0
	pairCount := 0

	regionPodCount := make(map[string]int)
	for _, pod := range pods {
		status := plugin.PreScore(ctx, state, pod, allNodes)
		if !status.IsSuccess() {
			t.Errorf("PreScore failed: %v", status.Message())
		}
		var maxScore int64 = -1
		var selectedNode string
		for _, node := range allNodes {
			score, status := plugin.Score(ctx, state, pod, node.Name)
			if !status.IsSuccess() {
				t.Errorf("Score failed: %v", status.Message())
			} else {
				t.Logf("Pod %s on Node %s got Score %d", pod.Name, node.Name, score)
			}

			if score > maxScore {
				maxScore = score
				selectedNode = node.Name
			}
		}

		pod.Spec.NodeName = selectedNode

		t.Logf("Scheduled Pod %s on Node %s", pod.Name, selectedNode)
		_, err := client.CoreV1().Pods(pod.Namespace).Update(ctx, pod, metav1.UpdateOptions{})
		if err != nil {
			t.Errorf("Failed to update pod: %v", err)
		}

		nodeInfo := nodeInfoMap[selectedNode]
		nodeInfo.AddPod(pod)
		nodeInfo.Requested.MilliCPU += pod.Spec.Containers[0].Resources.Requests.Cpu().MilliValue()
		nodeInfo.Requested.Memory += pod.Spec.Containers[0].Resources.Requests.Memory().Value()

		node := allNodes[getNodeIndex(allNodes, selectedNode)]
		cpuAlloc := node.Status.Allocatable[v1.ResourceCPU]
		memAlloc := node.Status.Allocatable[v1.ResourceMemory]

		cpuAlloc.Sub(*pod.Spec.Containers[0].Resources.Requests.Cpu())
		memAlloc.Sub(*pod.Spec.Containers[0].Resources.Requests.Memory())

		node.Status.Allocatable[v1.ResourceCPU] = cpuAlloc
		node.Status.Allocatable[v1.ResourceMemory] = memAlloc
		_, err = client.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
		if err != nil {
			// TODO
		}
		region := node.Labels["region"]
		regionPodCount[region]++
	}

	fmt.Printf("nodeScores: %v\n", nodeScores)

	for i := 0; i < len(pods); i++ {
		for j := i + 1; j < len(pods); j++ {
			pod1 := pods[i]
			pod2 := pods[j]

			node1 := allNodes[getNodeIndex(allNodes, pod1.Spec.NodeName)]
			node2 := allNodes[getNodeIndex(allNodes, pod2.Spec.NodeName)]

			if node1 != nil && node2 != nil {
				latency := getNodeLatency(node1, node2)
				allLatencies = append(allLatencies, latency)
				totalLatency += latency
				pairCount++
			}
		}
	}

	averageLatency := totalLatency / float64(pairCount)
	sort.Float64s(allLatencies)
	var medianLatency float64
	if len(allLatencies) > 0 {
		if len(allLatencies)%2 == 0 {
			mid := len(allLatencies) / 2
			medianLatency = (allLatencies[mid-1] + allLatencies[mid]) / 2
		} else {
			medianLatency = allLatencies[len(allLatencies)/2]
		}
	}

	fmt.Printf("medianLatency: %v\n", medianLatency)
	fmt.Printf("averageLatency: %v\n", averageLatency)

	return
}

// createNodesInRegion creates a specified number of nodes in a given region
func createNodesInRegion(region string, startIndex, count int, cpu int64, memory int64) []*v1.Node {
	nodes := make([]*v1.Node, count)
	for i := 0; i < count; i++ {
		nodeName := fmt.Sprintf("%s-node%d", region, startIndex+i)
		nodes[i] = mockNode(nodeName, cpu, memory, region)
		nodes[i].Labels = map[string]string{
			"region": region,
		}
	}
	return nodes
}

func getNodeIndex(nodes []*v1.Node, nodeName string) int {
	for i, node := range nodes {
		if node.Name == nodeName {
			return i
		}
	}
	return -1
}

// getNodeLatency returns the latency between two nodes based on their regions
func getNodeLatency(sourceNode, targetNode *v1.Node) float64 {
	sourceRegion := sourceNode.Labels["region"]
	targetRegion := targetNode.Labels["region"]
	return RegionLatencyMap[sourceRegion][targetRegion]
}

// createLatencyConfigMaps generates ConfigMaps with latency data based on node regions
func createLatencyConfigMaps(client *clientsetfake.Clientset) {

	nodes, _ := client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})

	latencyData := make(map[string]string)

	for _, sourceNode := range nodes.Items {
		latencies := make(map[string]float64)

		// Calculate latency to all other nodes
		for _, targetNode := range nodes.Items {
			if sourceNode.Name != targetNode.Name {
				latencies[targetNode.Name] = getNodeLatency(&sourceNode, &targetNode)
			}
		}

		// Convert latency map to JSON
		jsonData, _ := json.Marshal(latencies)
		latencyData[sourceNode.Name+"-latency"] = string(jsonData)

		// Create ConfigMap
		cm := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      sourceNode.Name + "-latency",
				Namespace: "default",
				Labels: map[string]string{
					"latency-data": "true",
				},
			},
			Data: map[string]string{
				"latency.json": string(jsonData),
			},
		}

		_, err := client.CoreV1().ConfigMaps("default").Create(context.TODO(), cm, metav1.CreateOptions{})
		if err != nil {
			//TODO Handle error
		}
	}
}

func newFakeSharedLister(pods []*v1.Pod, nodeInfoMap map[string]*framework.NodeInfo) framework.SharedLister {
	return &fakeSharedLister{
		pods:        pods,
		nodeInfoMap: nodeInfoMap,
	}
}

type fakeSharedLister struct {
	pods        []*v1.Pod
	nodeInfoMap map[string]*framework.NodeInfo
}

func (f *fakeSharedLister) NodeInfos() framework.NodeInfoLister {
	return fakeNodeInfoLister(f.nodeInfoMap)
}

func (f *fakeSharedLister) StorageInfos() framework.StorageInfoLister {
	return nil
}

type fakeNodeInfoLister map[string]*framework.NodeInfo

func (f fakeNodeInfoLister) HavePodsWithRequiredAntiAffinityList() ([]*framework.NodeInfo, error) {
	//TODO implement me
	panic("implement me")
}

func (f fakeNodeInfoLister) List() ([]*framework.NodeInfo, error) {
	var nodeInfos []*framework.NodeInfo
	for _, ni := range f {
		nodeInfos = append(nodeInfos, ni)
	}
	return nodeInfos, nil
}

func (f fakeNodeInfoLister) HavePodsWithAffinityList() ([]*framework.NodeInfo, error) {
	return nil, nil
}

func (f fakeNodeInfoLister) Get(nodeName string) (*framework.NodeInfo, error) {
	if ni, ok := f[nodeName]; ok {
		return ni, nil
	}
	return nil, framework.NewStatus(framework.Error, "node not found").AsError()
}

func mockNode(name string, cpu int64, memory int64, region string) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"topology.kubernetes.io/region": region}},
		Spec: v1.NodeSpec{
			PodCIDR:       "",
			PodCIDRs:      nil,
			ProviderID:    "",
			Unschedulable: false,
			Taints:        nil,
			ConfigSource: &v1.NodeConfigSource{
				ConfigMap: &v1.ConfigMapNodeConfigSource{
					Namespace:        "",
					Name:             name,
					UID:              "",
					ResourceVersion:  "",
					KubeletConfigKey: "",
				},
			},
			DoNotUseExternalID: "",
		},
		Status: v1.NodeStatus{
			Allocatable: v1.ResourceList{
				v1.ResourceCPU:    *resource.NewMilliQuantity(cpu, resource.DecimalSI),
				v1.ResourceMemory: *resource.NewQuantity(memory, resource.BinarySI),
			},
			Capacity: v1.ResourceList{
				v1.ResourceCPU:    *resource.NewMilliQuantity(cpu, resource.DecimalSI),
				v1.ResourceMemory: *resource.NewQuantity(memory, resource.BinarySI),
			},
		},
	}
}

func mockPod(name string, cpu int64, memory int64, group string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Annotations: map[string]string{
				PodGroupAnnotation: group,
			}},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceCPU:    *resource.NewMilliQuantity(cpu, resource.DecimalSI),
							v1.ResourceMemory: *resource.NewQuantity(memory, resource.BinarySI),
						},
					},
				},
			},
		},
	}
}
