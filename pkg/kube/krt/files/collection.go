package files

import (
	"fmt"
	"os"
	"time"

	"go.uber.org/atomic"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pkg/filewatcher"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
)

type Collection[T any] struct {
	krt.StaticCollection[T]
}

func NewCollection[T any](opts ...krt.CollectionOption) Collection[T] {
	panic("not yet implemented")
}

type Singleton[T any] struct {
	krt.Singleton[T]
}

// NewSingleton returns a collection that reads and watches a single file
// The `readFile` function is used to read and deserialize the file and will be called each time the file changes.
// This will also be called during the initial construction of the collection; if the initial readFile fails an error is returned.
func NewSingleton[T any](
	fileWatcher filewatcher.FileWatcher,
	filename string,
	stop <-chan struct{},
	readFile func(filename string) (T, error),
	opts ...krt.CollectionOption,
) (Singleton[T], error) {
	cfg, err := readFile(filename)
	if err != nil {
		return Singleton[T]{}, err
	}

	cur := atomic.NewPointer(&cfg)
	trigger := krt.NewRecomputeTrigger(true, opts...)
	sc := krt.NewSingleton[T](func(ctx krt.HandlerContext) *T {
		trigger.MarkDependant(ctx)
		return cur.Load()
	}, opts...)
	// TODO use proper stop.
	sc.AsCollection().Synced().WaitUntilSynced(stop)
	watchFile(fileWatcher, filename, stop, func() {
		cfg, err := readFile(filename)
		if err != nil {
			log.Warnf("failed to update: %v", err)
			return
		}
		cur.Store(&cfg)
		trigger.TriggerRecomputation()
	})
	return Singleton[T]{sc}, nil
}

func ReadFileAsYaml[T any](filename string) (T, error) {
	target := ptr.Empty[T]()
	y, err := os.ReadFile(filename)
	if err != nil {
		return target, fmt.Errorf("failed to read file %s: %v", filename, err)
	}
	if err := yaml.Unmarshal(y, &target); err != nil {
		return target, fmt.Errorf("failed to read file %s: %v", filename, err)
	}
	return target, nil
}

func watchFile(fileWatcher filewatcher.FileWatcher, file string, stop <-chan struct{}, callback func()) {
	_ = fileWatcher.Add(file)
	go func() {
		var timerC <-chan time.Time
		for {
			select {
			case <-stop:
				return
			case <-timerC:
				timerC = nil
				callback()
			case <-fileWatcher.Events(file):
				// Use a timer to debounce configuration updates
				if timerC == nil {
					timerC = time.After(100 * time.Millisecond)
				}
			}
		}
	}()
}
