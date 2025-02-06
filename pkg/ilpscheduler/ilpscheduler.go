package ilpscheduler

import (
	"context"
	"encoding/json"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	"math"
	"sigs.k8s.io/scheduler-plugins/pkg/linearprog"
	"strings"
	"sync"
)

const (
	Name               = "ILPScheduler"
	PodGroupAnnotation = "pod-group.scheduling.k8s.io"
	AlphaWeight        = 0.3 // Resource fragmentation
	BetaWeight         = 0.3 // Inter-pod latency
)

type LatencyData struct {
	Latencies map[string]map[string]float64
}

type ILPPlugin struct {
	handle      framework.Handle
	latencyMu   sync.RWMutex
	latencyData map[string]map[string]float64
	podGroup    map[string][]string
}

type RegionResources struct {
	AvailableCPU    int64
	AvailableMemory int64
	TotalCPU        int64
	TotalMemory     int64
	NodeCount       int
}

func New(ctx context.Context, obj runtime.Object, h framework.Handle) (framework.Plugin, error) {
	return &ILPPlugin{
		handle:      h,
		latencyData: make(map[string]map[string]float64),
		podGroup:    make(map[string][]string),
	}, nil
}

func (pl *ILPPlugin) Name() string {
	return Name
}

func (pl *ILPPlugin) PreScore(ctx context.Context, state *framework.CycleState, pod *corev1.Pod, nodes []*corev1.Node) *framework.Status {
	if err := pl.loadLatencyData(); err != nil {
		return framework.NewStatus(framework.Error, fmt.Sprintf("failed to load latency data: %v", err))
	}

	group := pod.Annotations[PodGroupAnnotation]
	if group == "" {
		return framework.NewStatus(framework.Success, "")
	}

	if _, exists := pl.podGroup[group]; !exists {
		pl.podGroup[group] = []string{}
	}
	pl.podGroup[group] = append(pl.podGroup[group], pod.Name)

	return framework.NewStatus(framework.Success, "")
}

func (pl *ILPPlugin) Score(ctx context.Context, state *framework.CycleState, pod *corev1.Pod, nodeName string) (int64, *framework.Status) {
	nodeInfo, err := pl.handle.SnapshotSharedLister().NodeInfos().Get(nodeName)
	if err != nil {
		return 0, framework.NewStatus(framework.Error, fmt.Sprintf("failed to get node info: %v", err))
	}

	score, err := pl.solveILP(pod, nodeInfo)
	if err != nil {
		return 0, framework.NewStatus(framework.Error, fmt.Sprintf("failed to solve ILP: %v", err))
	}

	fmt.Printf("normalized score: %d for pod: %s on node: %s\n", int64(score*100), pod.Name, nodeName)
	return int64(score * 100), framework.NewStatus(framework.Success, "")
}

func (pl *ILPPlugin) ScoreExtensions() framework.ScoreExtensions {
	return pl
}

func (pl *ILPPlugin) NormalizeScore(ctx context.Context, state *framework.CycleState, pod *corev1.Pod, scores framework.NodeScoreList) *framework.Status {
	var highest int64 = 0
	for _, nodeScore := range scores {
		if nodeScore.Score > highest {
			highest = nodeScore.Score
		}
	}

	if highest == 0 {
		return framework.NewStatus(framework.Success)
	}

	for i := range scores {
		scores[i].Score = scores[i].Score * framework.MaxNodeScore / highest
	}

	return framework.NewStatus(framework.Success)
}

func (pl *ILPPlugin) getRegionResources() (map[string]*RegionResources, error) {
	nodeInfos, err := pl.handle.SnapshotSharedLister().NodeInfos().List()
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %v", err)
	}

	resources := make(map[string]*RegionResources)

	for _, nodeInfo := range nodeInfos {
		node := nodeInfo.Node()
		if node == nil {
			continue
		}

		region := node.Labels["region"]
		if region == "" {
			continue
		}

		if _, exists := resources[region]; !exists {
			resources[region] = &RegionResources{}
		}

		cpuCap := node.Status.Capacity.Cpu().MilliValue()
		memCap := node.Status.Capacity.Memory().Value()
		cpuAlloc := nodeInfo.Requested.MilliCPU
		memAlloc := nodeInfo.Requested.Memory

		resources[region].AvailableCPU += (cpuCap - cpuAlloc)
		resources[region].AvailableMemory += (memCap - memAlloc)
		resources[region].TotalCPU += cpuCap
		resources[region].TotalMemory += memCap
		resources[region].NodeCount++
	}

	return resources, nil
}

func (pl *ILPPlugin) getBestRegion(resources map[string]*RegionResources, requiredCPU, requiredMemory int64) string {
	var bestRegion string
	var maxScore float64 = -1

	for region, res := range resources {
		if res.AvailableCPU < requiredCPU || res.AvailableMemory < requiredMemory {
			continue
		}

		cpuScore := float64(res.AvailableCPU) / float64(res.TotalCPU)
		memScore := float64(res.AvailableMemory) / float64(res.TotalMemory)
		score := (cpuScore + memScore) / 2

		if score > maxScore {
			maxScore = score
			bestRegion = region
		}
	}

	return bestRegion
}

func (pl *ILPPlugin) loadLatencyData() error {
	pl.latencyMu.Lock()
	defer pl.latencyMu.Unlock()
	configMapList, err := pl.handle.ClientSet().CoreV1().ConfigMaps("default").List(context.Background(), metav1.ListOptions{
		LabelSelector: "latency-data=true",
	})
	if err != nil {
		return fmt.Errorf("failed to load latency data: %v", err)
	}

	for _, cm := range configMapList.Items {
		nodeName := strings.TrimSuffix(cm.Name, "-latency")
		data := cm.Data["latency.json"]
		nodeLatencies := make(map[string]float64)
		err := json.Unmarshal([]byte(data), &nodeLatencies)
		if err != nil {
			fmt.Printf("Error unmarshaling JSON: %v\n", err)
			continue
		}
		pl.latencyData[nodeName] = nodeLatencies
	}
	return nil
}

func (pl *ILPPlugin) getGroupPods(podGroup string) ([]*corev1.Pod, error) {
	pods, err := pl.handle.ClientSet().CoreV1().Pods(corev1.NamespaceAll).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("error listing pods: %v", err)
	}

	var groupPods []*corev1.Pod
	for _, p := range pods.Items {
		if group, exists := p.Annotations[PodGroupAnnotation]; exists && group == podGroup && p.Spec.NodeName != "" {
			groupPods = append(groupPods, &p)
		}
	}

	return groupPods, nil
}

func (pl *ILPPlugin) solveILP(pod *corev1.Pod, nodeInfo *framework.NodeInfo) (float64, error) {
	nodeRegion := nodeInfo.Node().ObjectMeta.Labels["region"]
	if nodeRegion == "" {
		return 0, fmt.Errorf("node %s has no region label", nodeInfo.Node().Name)
	}

	groupPods, _ := pl.getGroupPods(pod.Annotations[PodGroupAnnotation])

	if groupPods == nil || len(groupPods) == 0 {
		cpuRequest := getResourceRequest(pod, corev1.ResourceCPU)
		memRequest := getResourceRequest(pod, corev1.ResourceMemory)

		regionResources, err := pl.getRegionResources()
		if err != nil {
			return 0, err
		}

		bestRegion := pl.getBestRegion(regionResources, cpuRequest, memRequest)

		if bestRegion == nodeRegion {
			return 1.0, nil
		}
		return 0.0, nil
	}

	objCoeff := []float64{AlphaWeight, AlphaWeight, BetaWeight}

	constraints := make(map[int]map[int]float64)
	var constraintRHS []float64
	var constraintDir []string

	cpuRequest := float64(getResourceRequest(pod, corev1.ResourceCPU))
	memRequest := float64(getResourceRequest(pod, corev1.ResourceMemory))
	cpuCapacity := float64(nodeInfo.Node().Status.Capacity.Cpu().MilliValue())
	memCapacity := float64(nodeInfo.Node().Status.Capacity.Memory().Value())
	cpuUsed := float64(nodeInfo.Requested.MilliCPU)
	memUsed := float64(nodeInfo.Requested.Memory)

	constraints[1] = map[int]float64{
		1: cpuRequest, // x1: CPU utilization
		2: 0,          // x2: Memory utilization
		3: 0,          // x3: Network latency score
		//4: 0,          // x4: Load balance score
	}
	constraintRHS = append(constraintRHS, cpuCapacity-cpuUsed)
	constraintDir = append(constraintDir, "<=")

	constraints[2] = map[int]float64{
		1: 0,          // x1: CPU utilization
		2: memRequest, // x2: Memory utilization
		3: 0,          // x3: Network latency score
		//4: 0,          // x4: Load balance score
	}
	constraintRHS = append(constraintRHS, memCapacity-memUsed)
	constraintDir = append(constraintDir, "<=")

	var avgLatency float64
	var totalLatency float64
	var podCount int
	for _, p := range groupPods {
		if p.Spec.NodeName != "" && p.Name != pod.Name {
			latency := pl.getLatency(nodeInfo.Node().Name, p.Spec.NodeName)
			totalLatency += latency
			podCount++
		}
	}
	if podCount > 0 {
		avgLatency = totalLatency / float64(podCount)
	}

	normalizedLatency := math.Exp(-avgLatency / 100.0)
	constraints[3] = map[int]float64{
		1: 0, // x1: CPU utilization
		2: 0, // x2: Memory utilization
		3: 1, // x3: Network latency score
		//4: 0, // x4: Load balance score
	}
	constraintRHS = append(constraintRHS, normalizedLatency)
	constraintDir = append(constraintDir, "<=")

	//Load Balancing Constraint
	//targetUtilization := 0.8
	//currentUtilization := (((cpuUsed + cpuRequest) / cpuCapacity) + ((memUsed + memRequest) / cpuCapacity)) / 2
	//loadBalanceScore := 1.0 - math.Abs(targetUtilization-currentUtilization)
	//constraints[4] = map[int]float64{
	//	1: 0, // x1: CPU utilization
	//	2: 0, // x2: Memory utilization
	//	3: 0, // x3: Network latency score
	//	4: 1, // x4: Load balance score
	//}
	//constraintRHS = append(constraintRHS, loadBalanceScore)
	//constraintDir = append(constraintDir, "<=")

	solution, _ := linearprog.Simplex(constraints, constraintRHS, objCoeff, constraintDir)

	var score float64
	for i := 0; i < len(objCoeff); i++ {
		if val, ok := solution[i]; ok {
			score += objCoeff[i] * val
		}
	}

	normalizedScore := score / float64(len(objCoeff))
	if normalizedScore < 0 {
		normalizedScore = 0
	} else if normalizedScore > 1 {
		normalizedScore = 1
	}

	return normalizedScore, nil
}

func (pl *ILPPlugin) getLatency(source, dest string) float64 {
	pl.latencyMu.RLock()
	defer pl.latencyMu.RUnlock()

	if sourceMap, ok := pl.latencyData[source]; ok {
		if latency, ok := sourceMap[dest]; ok {
			return latency
		}
	}
	return 0
}

func getResourceRequest(pod *corev1.Pod, resource corev1.ResourceName) int64 {
	var total int64
	for _, container := range pod.Spec.Containers {
		if request, ok := container.Resources.Requests[resource]; ok {
			total += request.Value()
		}
	}
	return total
}
