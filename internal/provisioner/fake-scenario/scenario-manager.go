package fakescenario

import (
	"context"
	"fmt"
	"math/rand/v2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type ScenarioManager struct {
	Client *kubernetes.Clientset
}

func (s *ScenarioManager) TriggerOverheating(nodeName string) error {
	cm, err := s.Client.CoreV1().ConfigMaps("monitoring").Get(context.TODO(), "dcgm-mock-data", metav1.GetOptions{})
	if err != nil {
		return err
	}
	gpuIdx := 0
	heat := rand.IntN(100)
	mem_bandwidth_utilization := rand.Float32()
	cm.Data["metrics"] = `
		dcgm_gpu_temp{node="` + nodeName + `", gpu="` + fmt.Sprint(gpuIdx) + `"} ` + fmt.Sprint(heat) + `
		dcgm_mem_copy_util{node="` + nodeName + `", gpu="` + fmt.Sprint(gpuIdx) + `"} ` + fmt.Sprint(mem_bandwidth_utilization) + `
	`
	_, err = s.Client.CoreV1().ConfigMaps("monitoring").Update(context.TODO(), cm, metav1.UpdateOptions{})
	return nil
}
