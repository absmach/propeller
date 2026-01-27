package standalone

import (
	"github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/pkg/orchestration"
	"github.com/absmach/propeller/pkg/orchestration/events"
	"github.com/absmach/propeller/pkg/orchestration/executor"
	"github.com/absmach/propeller/pkg/orchestration/store"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
)

type Adapter struct {
	StateStore   orchestration.StateStore
	WorkExecutor orchestration.WorkExecutor
	EventEmitter orchestration.EventEmitter
	Scheduler    orchestration.Scheduler
	TopicBuilder *orchestration.TopicBuilder
}

func NewAdapter(
	tasksDB, propletsDB, taskPropletDB, metricsDB storage.Storage,
	roundsDB storage.Storage, // Can be nil, will use metricsDB as fallback
	s scheduler.Scheduler,
	pubsub mqtt.PubSub,
	domainID, channelID string,
) *Adapter {
	// Use metricsDB as fallback for rounds if roundsDB is nil
	if roundsDB == nil {
		roundsDB = metricsDB
	}

	// Create state store
	stateStore := store.NewMemoryStateStore(tasksDB, propletsDB, taskPropletDB, roundsDB)

	// Create topic builder
	topicBuilder := orchestration.NewTopicBuilder(domainID, channelID)

	// Create work executor
	workExecutor := executor.NewMQTTWorkExecutor(
		pubsub,
		topicBuilder.ManagerStartTopic(),
		topicBuilder.ManagerStopTopic(),
	)

	// Create event emitter
	eventEmitter := events.NewMQTTEventEmitter(pubsub, topicBuilder)

	// Create scheduler adapter - wrap the existing scheduler
	schedulerAdapter := orchestration.NewLegacySchedulerAdapter(s)

	return &Adapter{
		StateStore:   stateStore,
		WorkExecutor: workExecutor,
		EventEmitter: eventEmitter,
		Scheduler:    schedulerAdapter,
		TopicBuilder: topicBuilder,
	}
}
