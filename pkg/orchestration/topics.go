package orchestration

import "fmt"

type TopicBuilder struct {
	domainID  string
	channelID string
}

func NewTopicBuilder(domainID, channelID string) *TopicBuilder {
	return &TopicBuilder{
		domainID:  domainID,
		channelID: channelID,
	}
}

func (tb *TopicBuilder) BaseTopic() string {
	return fmt.Sprintf("m/%s/c/%s", tb.domainID, tb.channelID)
}

func (tb *TopicBuilder) ManagerStartTopic() string {
	return tb.BaseTopic() + "/control/manager/start"
}

func (tb *TopicBuilder) ManagerStopTopic() string {
	return tb.BaseTopic() + "/control/manager/stop"
}

func (tb *TopicBuilder) PropletCreateTopic() string {
	return tb.BaseTopic() + "/control/proplet/create"
}

func (tb *TopicBuilder) PropletAliveTopic() string {
	return tb.BaseTopic() + "/control/proplet/alive"
}

func (tb *TopicBuilder) PropletResultsTopic() string {
	return tb.BaseTopic() + "/control/proplet/results"
}

func (tb *TopicBuilder) PropletTaskMetricsTopic() string {
	return tb.BaseTopic() + "/control/proplet/task_metrics"
}

func (tb *TopicBuilder) PropletMetricsTopic() string {
	return tb.BaseTopic() + "/control/proplet/metrics"
}

func (tb *TopicBuilder) FLRoundStartTopic() string {
	return tb.BaseTopic() + "/fl/rounds/start"
}

func (tb *TopicBuilder) FLRoundUpdateTopic(roundID, propletID string) string {
	return fmt.Sprintf("fl/rounds/%s/updates/%s", roundID, propletID)
}

func (tb *TopicBuilder) FLRoundNextTopic() string {
	return "fl/rounds/next"
}

func (tb *TopicBuilder) AllTopics() string {
	return tb.BaseTopic() + "/#"
}
