package sascheduling

import (
	"context"
	"encoding/json"
	"fmt"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	"math"
	"math/rand"
	"strings"
	"sync"
)

const (
	Name               = "SAScheduler"
	PodGroupAnnotation = "pod-group.scheduling.k8s.io"
	InitialTemperature = 100.0
	CoolingRate        = 0.95
	MinTemperature     = 0.1
)

type RegionResources struct {
	AvailableCPU    int64
	AvailableMemory int64
	TotalCPU        int64
	TotalMemory     int64
	NodeCount       int
}

type SAPlugin struct {
	handle      framework.Handle
	latencyData map[string]map[string]float64
	groupPods   map[string][]*v1.Pod
	mu          sync.RWMutex
}

func (pl *SAPlugin) Name() string {
	return Name
}

func New(ctx context.Context, _ runtime.Object, h framework.Handle) (framework.Plugin, error) {
	return &SAPlugin{
		handle:      h,
		latencyData: make(map[string]map[string]float64),
		groupPods:   make(map[string][]*v1.Pod),
	}, nil
}

func (pl *SAPlugin) PreScore(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodes []*v1.Node) *framework.Status {
	podGroup, exists := pod.Annotations[PodGroupAnnotation]
	if !exists {
		return framework.NewStatus(framework.Success)
	}

	if err := pl.loadLatencyData(ctx); err != nil {
		return framework.NewStatus(framework.Error, err.Error())
	}

	pods, err := pl.getPodsInGroup(ctx, podGroup)
	if err != nil {
		return framework.NewStatus(framework.Error, err.Error())
	}
	pl.groupPods[podGroup] = pods

	return framework.NewStatus(framework.Success)
}

func (pl *SAPlugin) Score(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) (int64, *framework.Status) {
	podGroup, exists := pod.Annotations[PodGroupAnnotation]
	if !exists {
		return 0, framework.NewStatus(framework.Success)
	}

	pl.mu.RLock()
	defer pl.mu.RUnlock()

	node, err := pl.handle.SnapshotSharedLister().NodeInfos().Get(nodeName)
	if err != nil {
		return 0, framework.NewStatus(framework.Success)
	}
	podRequestedCPU := pod.Spec.Containers[0].Resources.Requests.Cpu().MilliValue()
	podRequestedMemory := pod.Spec.Containers[0].Resources.Requests.Memory().Value()

	allocatableCPU := node.Node().Status.Allocatable.Cpu().MilliValue()
	allocatableMemory := node.Node().Status.Allocatable.Memory().Value()

	newCPUUsage := float64(node.Requested.MilliCPU+podRequestedCPU) / float64(allocatableCPU)
	newMemoryUsage := float64(node.Requested.Memory+podRequestedMemory) / float64(allocatableMemory)

	if newCPUUsage > 0.8 || newMemoryUsage > 0.8 {
		return 0, framework.NewStatus(framework.Success)
	}
	score := pl.simulatedAnnealing(pod, node, podGroup)

	normalizedScore := int64(math.Round(score * float64(framework.MaxNodeScore)))
	if normalizedScore > framework.MaxNodeScore {
		normalizedScore = framework.MaxNodeScore
	}
	if normalizedScore < 0 {
		normalizedScore = 0
	}
	//fmt.Printf("normalized score: %d for pod: %s on node: %s\n", normalizedScore, pod.Name, nodeName)
	return normalizedScore, framework.NewStatus(framework.Success)
}

func (pl *SAPlugin) ScoreExtensions() framework.ScoreExtensions {
	return pl
}

func (pl *SAPlugin) NormalizeScore(ctx context.Context, state *framework.CycleState, pod *v1.Pod, scores framework.NodeScoreList) *framework.Status {
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

func (pl *SAPlugin) simulatedAnnealing(pod *v1.Pod, node *framework.NodeInfo, podGroup string) float64 {

	temperature := InitialTemperature
	currentScore := pl.calculateEnergy(pod, node, podGroup)
	bestScore := currentScore
	iteration := 0

	for temperature > MinTemperature {
		iteration++

		neighborScore := pl.calculateEnergy(pod, node, podGroup)

		delta := neighborScore - currentScore
		if delta < 0 || rand.Float64() < math.Exp(-delta/temperature) {
			currentScore = neighborScore
			if currentScore < bestScore {
				bestScore = currentScore
			}
		}
		temperature *= CoolingRate
	}

	return 1.0 / (1.0 + bestScore)
}

func (pl *SAPlugin) calculateEnergy(pod *v1.Pod, node *framework.NodeInfo, podGroup string) float64 {

	podRequestedCPU := pod.Spec.Containers[0].Resources.Requests.Cpu().MilliValue()
	podRequestedMemory := pod.Spec.Containers[0].Resources.Requests.Memory().Value()

	allocatableCPU := node.Node().Status.Allocatable.Cpu().MilliValue()
	allocatableMemory := node.Node().Status.Allocatable.Memory().Value()

	availableCPU := allocatableCPU - node.Requested.MilliCPU
	availableMemory := allocatableMemory - node.Requested.Memory

	if podRequestedCPU > availableCPU || podRequestedMemory > availableMemory {
		return math.MaxFloat64
	}

	newCPUUsage := float64(node.Requested.MilliCPU+podRequestedCPU) / float64(allocatableCPU)
	newMemoryUsage := float64(node.Requested.Memory+podRequestedMemory) / float64(allocatableMemory)

	if newCPUUsage > 0.8 || newMemoryUsage > 0.8 {
		return math.MaxFloat64
	}

	var totalLatency float64
	for _, groupPod := range pl.groupPods[podGroup] {
		if groupPod.Spec.NodeName != "" {
			if latency, exists := pl.latencyData[node.Node().Name][groupPod.Spec.NodeName]; exists {
				totalLatency += latency
			}
		}
	}

	cpuUtilization := float64(podRequestedCPU) / float64(availableCPU)
	memoryUtilization := float64(podRequestedMemory) / float64(availableMemory)
	totalResourceUtilization := (cpuUtilization + memoryUtilization) / 2.0

	return 0.5*totalLatency + 0.5*totalResourceUtilization
}

func (pl *SAPlugin) getRegionResources() (map[string]*RegionResources, error) {
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

		resources[region].AvailableCPU += cpuCap - cpuAlloc
		resources[region].AvailableMemory += memCap - memAlloc
		resources[region].TotalCPU += cpuCap
		resources[region].TotalMemory += memCap
		resources[region].NodeCount++
	}

	return resources, nil
}

func (pl *SAPlugin) getBestRegion(resources map[string]*RegionResources, requiredCPU, requiredMemory int64) string {
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

func (pl *SAPlugin) loadLatencyData(ctx context.Context) error {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	configMapList, err := pl.handle.ClientSet().CoreV1().ConfigMaps("default").List(ctx, metav1.ListOptions{
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

func (pl *SAPlugin) getPodsInGroup(ctx context.Context, podGroup string) ([]*v1.Pod, error) {
	pods, err := pl.handle.ClientSet().CoreV1().Pods(v1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("error listing pods: %v", err)
	}

	var groupPods []*v1.Pod
	for _, p := range pods.Items {
		if group, exists := p.Annotations[PodGroupAnnotation]; exists && group == podGroup {
			groupPods = append(groupPods, &p)
		}
	}

	return groupPods, nil
}
