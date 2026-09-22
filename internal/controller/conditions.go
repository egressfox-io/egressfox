package controller

import (
	"reflect"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ConditionReady              = "Ready"
	ConditionSourcesReady       = "SourcesReady"
	ConditionProbeDataReady     = "ProbeDataReady"
	ConditionSelectionReady     = "SelectionReady"
	ConditionConfigurationValid = "ConfigurationValid"
	ConditionPublished          = "Published"
	ConditionActivated          = "Activated"
	ConditionRuntimeReady       = "RuntimeReady"
	ConditionDegraded           = "Degraded"
)

func condition(kind string, status metav1.ConditionStatus, reason, message string, generation int64, now time.Time) metav1.Condition {
	return metav1.Condition{Type: kind, Status: status, Reason: reason, Message: message,
		ObservedGeneration: generation, LastTransitionTime: metav1.NewTime(now.UTC())}
}

func sameStatus(left, right any) bool { return reflect.DeepEqual(left, right) }
