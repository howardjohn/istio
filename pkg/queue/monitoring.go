package queue

import "istio.io/istio/pkg/monitoring"

var (
	nameTag = monitoring.MustCreateLabel("name")

	queue = monitoring.NewGauge(
		"workqueue_depth",
		"number of items in the queue.",
		monitoring.WithLabels(nameTag),
	)

	callBackTime = monitoring.NewDistribution(
		"callback_time",
		"duration it takes to call the dequed callback",
		[]float64{.01, .1, 1, 3, 5, 10, 20, 30},
		monitoring.WithLabels(nameTag),
	)
)

func init() {
	monitoring.MustRegister(
		queue,
		callBackTime,
	)
}
