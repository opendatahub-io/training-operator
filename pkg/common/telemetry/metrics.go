package telemetry

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	kubeflowv1 "github.com/kubeflow/training-operator/pkg/apis/kubeflow.org/v1"
)

var (
	once      sync.Once
	k8sClient client.Client
	mu        sync.RWMutex

	trainingJobsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "training_jobs_total",
			Help: "Total training jobs created by image type and runtime version",
		},
		[]string{"image_type", "runtime_version"},
	)

	//real-time gauge for current CRD instances
	pytorchCRDInstances = prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "pytorch_crd_instances",
			Help: "Current number of PyTorchJob CRD instances in the cluster",
		},
		countPyTorchJobs,
	)

	// Customer classification info metric
	trainingCustomerClassInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "training_customer_class_info",
			Help: "Customer classification (prod, dev, ci, unknown) with value 1",
		},
		[]string{"class"},
	)

	//health metric
	telemetryErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "telemetry_errors_total",
			Help: "Errors encountered during telemetry collection",
		},
	)
)

// InitMetrics initializes all metrics with the provided K8s client
func InitMetrics(c client.Client) {
	once.Do(func() {
		//Set the client before registering metrics
		mu.Lock()
		k8sClient = c
		mu.Unlock()

		metrics.Registry.MustRegister(
			trainingJobsTotal,
			pytorchCRDInstances,
			trainingCustomerClassInfo,
			telemetryErrors,
		)

		//set customer class info
		customerClass := getCustomerClass()
		trainingCustomerClassInfo.WithLabelValues(customerClass).Set(1)
	})
}

func countPyTorchJobs() float64 {
	mu.RLock()
	c := k8sClient
	mu.RUnlock()

	if c == nil {
		return 0
	}

	//short timeout to avoid stalling endpoint
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	var list kubeflowv1.PyTorchJobList
	if err := c.List(ctx, &list); err != nil {
		RecordTelemetryError()
		return 0
	}

	return float64(len(list.Items))
}

func getCustomerClass() string {
	class := strings.ToLower(strings.TrimSpace(os.Getenv("KFTO_CUSTOMER_CLASS")))
	switch class {
	case "prod", "production":
		return "prod"
	case "dev", "development":
		return "dev"
	case "ci", "test", "testing":
		return "ci"
	default:
		return "unknown"
	}
}

func RecordTelemetryError() {
	telemetryErrors.Inc()
}
