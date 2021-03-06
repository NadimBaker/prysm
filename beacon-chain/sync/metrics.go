package sync

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	pb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/beacon-chain/p2p"
)

var (
	topicPeerCount = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "p2p_topic_peer_count",
			Help: "The number of peers subscribed to a given topic.",
		}, []string{"topic"},
	)
	messageReceivedCounter = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "p2p_message_received_total",
			Help: "Count of messages received.",
		},
		[]string{"topic"},
	)
	messageFailedValidationCounter = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "p2p_message_failed_validation_total",
			Help: "Count of messages that failed validation.",
		},
		[]string{"topic"},
	)
	messageFailedProcessingCounter = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "p2p_message_failed_processing_total",
			Help: "Count of messages that passed validation but failed processing.",
		},
		[]string{"topic"},
	)
	numberOfTimesResyncedCounter = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "number_of_times_resynced",
			Help: "Count the number of times a node resyncs.",
		},
	)
	numberOfBlocksRecoveredFromAtt = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "beacon_blocks_recovered_from_attestation_total",
			Help: "Count the number of times a missing block recovered from attestation vote.",
		},
	)
	numberOfBlocksNotRecoveredFromAtt = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "beacon_blocks_not_recovered_from_attestation_total",
			Help: "Count the number of times a missing block not recovered and pruned from attestation vote.",
		},
	)
	numberOfAttsRecovered = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "beacon_attestations_recovered_total",
			Help: "Count the number of times attestation recovered because of missing block",
		},
	)
	numberOfAttsNotRecovered = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "beacon_attestations_not_recovered_total",
			Help: "Count the number of times attestation not recovered and pruned because of missing block",
		},
	)
)

func (r *Service) updateMetrics() {
	// do not update metrics if genesis time
	// has not been initialized
	if r.chain.GenesisTime().IsZero() {
		return
	}
	// We update the dynamic subnet topics.
	digest, err := r.forkDigest()
	if err != nil {
		log.WithError(err).Errorf("Could not compute fork digest")
	}
	indices := r.aggregatorCommitteeIndices(r.chain.CurrentSlot())
	attTopic := p2p.GossipTypeMapping[reflect.TypeOf(&pb.Attestation{})]
	attTopic += r.p2p.Encoding().ProtocolSuffix()
	for _, committeeIdx := range indices {
		formattedTopic := fmt.Sprintf(attTopic, digest, committeeIdx)
		topicPeerCount.WithLabelValues(formattedTopic).Set(float64(len(r.p2p.PubSub().ListPeers(formattedTopic))))
	}
	// We update all other gossip topics.
	for topic := range p2p.GossipTopicMappings {
		// We already updated attestation subnet topics.
		if strings.Contains(topic, "committee_index") {
			continue
		}
		topic += r.p2p.Encoding().ProtocolSuffix()
		if !strings.Contains(topic, "%x") {
			topicPeerCount.WithLabelValues(topic).Set(float64(len(r.p2p.PubSub().ListPeers(topic))))
			continue
		}
		formattedTopic := fmt.Sprintf(topic, digest)
		topicPeerCount.WithLabelValues(formattedTopic).Set(float64(len(r.p2p.PubSub().ListPeers(formattedTopic))))
	}
}
