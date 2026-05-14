// Copyright 2026, Versioneer (https://versioneer.at)
// SPDX-License-Identifier: Apache-2.0

package authserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	pkgv1beta1 "github.com/versioneer-tech/lakefs-oss-contrib/api/v1beta1"
)

func TestCreateUserCreatesUser(t *testing.T) {
	k8sClient := newFakeClient(t)
	server := New(k8sClient, ":0", WithDefaultUserNamespace("lakefs-users"))

	body := bytes.NewBufferString(`{"username":"admin","friendly_name":"Administrator"}`)
	request := httptest.NewRequest(http.MethodPost, "/auth/v1/auth/users", body)
	response := httptest.NewRecorder()

	server.routes().ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusCreated, response.Code, response.Body.String())
	}

	user := &pkgv1beta1.LakeFSUser{}
	if err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: "lakefs-users", Name: "admin"}, user); err != nil {
		t.Fatalf("expected LakeFSUser to be created: %v", err)
	}
	if user.Spec.ExternalID != "admin" {
		t.Fatalf("expected external ID admin, got %q", user.Spec.ExternalID)
	}
	if user.Spec.FriendlyName != "Administrator" {
		t.Fatalf("expected friendly name Administrator, got %q", user.Spec.FriendlyName)
	}
}

func TestCreateUserCredentialHonorsProvidedSecret(t *testing.T) {
	user := &pkgv1beta1.LakeFSUser{
		ObjectMeta: metav1.ObjectMeta{Name: "admin", Namespace: "lakefs-users"},
		Spec: pkgv1beta1.LakeFSUserSpec{
			ExternalID: "admin",
		},
	}
	k8sClient := newFakeClient(t, user)
	server := New(k8sClient, ":0", WithDefaultUserNamespace("lakefs-users"))

	request := httptest.NewRequest(
		http.MethodPost,
		"/auth/v1/auth/users/admin/credentials?access_key=lakefs_ak_admin&secret_key=lakefs_sk_admin",
		nil,
	)
	response := httptest.NewRecorder()

	server.routes().ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusCreated, response.Code, response.Body.String())
	}

	var payload credentialsWithSecret
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.AccessKeyID != "lakefs_ak_admin" || payload.SecretAccessKey != "lakefs_sk_admin" {
		t.Fatalf("unexpected credential response: %#v", payload)
	}

	credential := &pkgv1beta1.LakeFSCredential{}
	key := types.NamespacedName{Namespace: "lakefs-users", Name: "lakefs-cred-lakefs-ak-admin"}
	if err := k8sClient.Get(context.Background(), key, credential); err != nil {
		t.Fatalf("expected LakeFSCredential to be created: %v", err)
	}

	secret := &corev1.Secret{}
	if err := k8sClient.Get(context.Background(), key, secret); err != nil {
		t.Fatalf("expected Secret to be created: %v", err)
	}
	if string(secret.Data["accessKeyId"]) != "lakefs_ak_admin" {
		t.Fatalf("expected access key in Secret, got %q", string(secret.Data["accessKeyId"]))
	}
	if string(secret.Data["secretAccessKey"]) != "lakefs_sk_admin" {
		t.Fatalf("expected secret key in Secret, got %q", string(secret.Data["secretAccessKey"]))
	}
}

func TestGroupPolicyBindingAppliesToUserPolicies(t *testing.T) {
	user := &pkgv1beta1.LakeFSUser{
		ObjectMeta: metav1.ObjectMeta{Name: "writer", Namespace: "lakefs-users"},
		Spec: pkgv1beta1.LakeFSUserSpec{
			ExternalID: "writer",
		},
	}
	role := &pkgv1beta1.LakeFSRole{
		ObjectMeta: metav1.ObjectMeta{Name: "owner", Namespace: "lakefs-users"},
		Spec: pkgv1beta1.LakeFSRoleSpec{
			Policies: []pkgv1beta1.LakeFSPolicyTemplate{{
				Name: "<REPOSITORY>-owner",
				Statement: []pkgv1beta1.LakeFSPolicyStatement{{
					Effect:   "allow",
					Action:   []string{"fs:*"},
					Resource: "arn:lakefs:fs:::repository/<REPOSITORY>",
				}},
			}},
		},
	}
	k8sClient := newFakeClient(t, user, role)
	server := New(k8sClient, ":0", WithDefaultUserNamespace("lakefs-users"))

	createGroup := httptest.NewRequest(http.MethodPost, "/auth/v1/auth/groups", bytes.NewBufferString(`{"id":"pipeline"}`))
	createGroupResponse := httptest.NewRecorder()
	server.routes().ServeHTTP(createGroupResponse, createGroup)
	if createGroupResponse.Code != http.StatusCreated {
		t.Fatalf("expected group creation status %d, got %d with body %s", http.StatusCreated, createGroupResponse.Code, createGroupResponse.Body.String())
	}

	addMember := httptest.NewRequest(http.MethodPut, "/auth/v1/auth/groups/pipeline/members/writer", nil)
	addMemberResponse := httptest.NewRecorder()
	server.routes().ServeHTTP(addMemberResponse, addMember)
	if addMemberResponse.Code != http.StatusCreated {
		t.Fatalf("expected member creation status %d, got %d with body %s", http.StatusCreated, addMemberResponse.Code, addMemberResponse.Body.String())
	}

	attachPolicy := httptest.NewRequest(http.MethodPut, "/auth/v1/auth/groups/pipeline/policies/repo-a-owner?repository=repo-a", nil)
	attachPolicyResponse := httptest.NewRecorder()
	server.routes().ServeHTTP(attachPolicyResponse, attachPolicy)
	if attachPolicyResponse.Code != http.StatusCreated {
		t.Fatalf("expected policy attach status %d, got %d with body %s", http.StatusCreated, attachPolicyResponse.Code, attachPolicyResponse.Body.String())
	}

	request := httptest.NewRequest(http.MethodGet, "/auth/v1/auth/users/writer/policies", nil)
	response := httptest.NewRecorder()
	server.routes().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, response.Code, response.Body.String())
	}

	var payload paginatedResponse[policy]
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Results) != 1 {
		t.Fatalf("expected one effective policy, got %#v", payload.Results)
	}
	if payload.Results[0].Name != "repo-a-owner" {
		t.Fatalf("expected repository-scoped policy name, got %q", payload.Results[0].Name)
	}
	if payload.Results[0].Statement[0].Resource != "arn:lakefs:fs:::repository/repo-a" {
		t.Fatalf("expected repository-scoped resource, got %q", payload.Results[0].Statement[0].Resource)
	}
}

func TestAddBuiltInGroupMembershipCreatesGroup(t *testing.T) {
	user := &pkgv1beta1.LakeFSUser{
		ObjectMeta: metav1.ObjectMeta{Name: "admin", Namespace: "lakefs-users"},
		Spec: pkgv1beta1.LakeFSUserSpec{
			ExternalID: "admin",
		},
	}
	k8sClient := newFakeClient(t, user)
	server := New(k8sClient, ":0", WithDefaultUserNamespace("lakefs-users"))

	request := httptest.NewRequest(http.MethodPut, "/auth/v1/auth/groups/Admins/members/admin", nil)
	response := httptest.NewRecorder()
	server.routes().ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusCreated, response.Code, response.Body.String())
	}

	group := &pkgv1beta1.LakeFSGroup{}
	key := types.NamespacedName{Namespace: "lakefs-users", Name: "group-admins"}
	if err := k8sClient.Get(context.Background(), key, group); err != nil {
		t.Fatalf("expected LakeFSGroup to be created: %v", err)
	}
	if group.Spec.ExternalID != "Admins" {
		t.Fatalf("expected external ID Admins, got %q", group.Spec.ExternalID)
	}
	if len(group.Spec.Users) != 1 || group.Spec.Users[0].Name != "admin" {
		t.Fatalf("expected admin group membership, got %#v", group.Spec.Users)
	}
}

func newFakeClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := pkgv1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("add pkg scheme: %v", err)
	}

	builder := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithIndex(&pkgv1beta1.LakeFSUser{}, pkgv1beta1.UserExternalIDField, func(rawObj client.Object) []string {
			user := rawObj.(*pkgv1beta1.LakeFSUser)
			if user.Spec.ExternalID == "" {
				return nil
			}
			return []string{user.Spec.ExternalID}
		}).
		WithIndex(&pkgv1beta1.LakeFSGroup{}, pkgv1beta1.GroupExternalIDField, func(rawObj client.Object) []string {
			group := rawObj.(*pkgv1beta1.LakeFSGroup)
			if group.Spec.ExternalID == "" {
				return nil
			}
			return []string{group.Spec.ExternalID}
		}).
		WithIndex(&pkgv1beta1.LakeFSCredential{}, pkgv1beta1.CredentialAccessKeyIDField, func(rawObj client.Object) []string {
			credential := rawObj.(*pkgv1beta1.LakeFSCredential)
			if credential.Spec.AccessKeyID == "" {
				return nil
			}
			return []string{credential.Spec.AccessKeyID}
		}).
		WithIndex(&pkgv1beta1.LakeFSCredential{}, pkgv1beta1.CredentialUserField, func(rawObj client.Object) []string {
			credential := rawObj.(*pkgv1beta1.LakeFSCredential)
			if credential.Spec.UserRef.Name == "" {
				return nil
			}
			return []string{credential.Spec.UserRef.Name}
		})
	return builder.Build()
}
