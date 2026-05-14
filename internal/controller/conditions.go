// Copyright 2026, Versioneer (https://versioneer.at)
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pkgv1beta1 "github.com/versioneer-tech/lakefs-oss-contrib/api/v1beta1"
)

const (
	ConditionReady    = "Ready"
	ReasonResolved    = "Resolved"
	ReasonInvalid     = "Invalid"
	ReasonUnavailable = "Unavailable"
)

func readyCondition(status metav1.ConditionStatus, reason, message string, generation int64) metav1.Condition {
	return metav1.Condition{
		Type:               ConditionReady,
		Status:             status,
		ObservedGeneration: generation,
		Reason:             reason,
		Message:            message,
	}
}

func metaSetStatusCondition(conditions *[]metav1.Condition, condition metav1.Condition) {
	meta.SetStatusCondition(conditions, condition)
}

func lakeFSUserUsername(user *pkgv1beta1.LakeFSUser) string {
	if user.Spec.ExternalID != "" {
		return user.Spec.ExternalID
	}
	if user.Namespace == "" {
		return user.Name
	}
	return strings.Join([]string{user.Namespace, user.Name}, ":")
}
