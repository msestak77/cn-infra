package itest

import (
	"testing"

	"github.com/ligato/cn-infra/core"
	"github.com/ligato/cn-infra/db/keyval/etcdv3"
	etcdmock "github.com/ligato/cn-infra/db/keyval/etcdv3/mocks"
	"github.com/ligato/cn-infra/flavors/etcdkafka"
	"github.com/ligato/cn-infra/httpmux/mock"
	"github.com/ligato/cn-infra/messaging/kafka"
	kafkamux "github.com/ligato/cn-infra/messaging/kafka/mux"
	"github.com/onsi/gomega"
)

type suiteKafkaEtcdFlavor struct {
	T *testing.T
	AgentT
	Given
	When
	Then
}

// Setup registers gomega and starts the agent with the flavor argument
func (t *suiteKafkaEtcdFlavor) Setup(flavor core.Flavor, golangT *testing.T) {
	t.AgentT.Setup(flavor, t.t)
}

// MockEtcdKafkaFlavor initializes generic flavor with HTTP mock
//
// Example:
//
//     kafkamock, _, _ := kafkamux.Mock(t)
//     MockEtcdKafkaFlavor(T)
func MockEtcdKafkaFlavor(t *testing.T) (*etcdkafka.Flavor, *KafkaEtcdFlavorMocks) {
	genericFlavor, httpMock := MockGenericFlavor()
	kafkaMock := kafkamux.Mock(t)

	embededEtcd := etcdmock.Embedded{}
	embededEtcd.Start(t)
	defer embededEtcd.Stop()

	etcdClientLogger, err := genericFlavor.Logrus.NewLogger("etcdClient")
	if err != nil {
		panic(err)
	}
	etcdBytesCon, err := etcdv3.NewEtcdConnectionUsingClient(embededEtcd.Client(), etcdClientLogger)
	if err != nil {
		panic(err)
	}

	return &etcdkafka.Flavor{
		Generic: *genericFlavor,
		Etcd:    *etcdv3.FromExistingConnection(etcdBytesCon, genericFlavor.Logrus, &genericFlavor.ServiceLabel),
		Kafka:   *kafka.FromExistingMux(kafkaMock.Mux),
	}, &KafkaEtcdFlavorMocks{httpMock, kafkaMock}
}

// KafkaEtcdFlavorMocks
type KafkaEtcdFlavorMocks struct {
	*mock.HTTPMock
	KafkaMock *kafkamux.KafkaMock
}

// TC01 asserts that injection works fine and agent starts & stops
func (t *suiteKafkaEtcdFlavor) TC01StartStop() {
	flavor, _ := MockEtcdKafkaFlavor(t.T)
	t.Setup(flavor, t.T)
	defer t.Teardown()

	gomega.Expect(t.agent).ShouldNot(gomega.BeNil(), "agent is not initialized")
}
