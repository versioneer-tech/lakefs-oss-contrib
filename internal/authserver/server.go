// Copyright 2026, Versioneer (https://versioneer.at)
// SPDX-License-Identifier: Apache-2.0

package authserver

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pkgv1beta1 "github.com/versioneer-tech/lakefs-oss-contrib/api/v1beta1"
)

type Server struct {
	client               client.Client
	bindAddress          string
	defaultUserNamespace string
	log                  logrShim
}

type logrShim interface {
	Info(msg string, keysAndValues ...any)
	Error(err error, msg string, keysAndValues ...any)
}

type credentialsWithSecret struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	CreationDate    int64  `json:"creation_date"`
	UserID          int64  `json:"user_id"`
	UserName        string `json:"user_name"`
}

type credential struct {
	AccessKeyID  string `json:"access_key_id"`
	CreationDate int64  `json:"creation_date"`
}

type user struct {
	Username          string `json:"username"`
	CreationDate      int64  `json:"creation_date"`
	FriendlyName      string `json:"friendly_name,omitempty"`
	Email             string `json:"email,omitempty"`
	ExternalID        string `json:"external_id,omitempty"`
	EncryptedPassword string `json:"encryptedPassword,omitempty"`
}

type createUserRequest struct {
	Username     string  `json:"username"`
	FriendlyName *string `json:"friendly_name,omitempty"`
	Email        *string `json:"email,omitempty"`
	ExternalID   *string `json:"external_id,omitempty"`
	ExternalIDV2 *string `json:"externalId,omitempty"`
}

type group struct {
	ID           string `json:"id,omitempty"`
	Name         string `json:"name"`
	CreationDate int64  `json:"creation_date"`
	Description  string `json:"description,omitempty"`
}

type createGroupRequest struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

type policy struct {
	Name         string      `json:"name"`
	CreationDate int64       `json:"creation_date"`
	Statement    []statement `json:"statement"`
}

type statement struct {
	Effect   string   `json:"effect"`
	Resource string   `json:"resource"`
	Action   []string `json:"action"`
}

type paginatedResponse[T any] struct {
	Pagination pagination `json:"pagination"`
	Results    []T        `json:"results"`
}

type pagination struct {
	HasMore    bool   `json:"has_more"`
	NextOffset string `json:"next_offset,omitempty"`
	Results    int    `json:"results"`
	MaxPerPage int    `json:"max_per_page"`
}

type errorResponse struct {
	Message string `json:"message"`
}

var (
	safeObjectName = regexp.MustCompile(`[^a-z0-9.-]+`)
	builtinGroups  = map[string]struct{}{
		"Admins":     {},
		"SuperUsers": {},
		"Developers": {},
		"Viewers":    {},
	}
)

type Option func(*Server)

func WithDefaultUserNamespace(namespace string) Option {
	return func(s *Server) {
		if namespace != "" {
			s.defaultUserNamespace = namespace
		}
	}
}

func New(k8sClient client.Client, bindAddress string, options ...Option) *Server {
	server := &Server{
		client:               k8sClient,
		bindAddress:          bindAddress,
		defaultUserNamespace: "lakefs-oss-contrib-system",
		log:                  ctrl.Log.WithName("authserver"),
	}
	for _, option := range options {
		option(server)
	}
	return server
}

func (s *Server) Start(ctx context.Context) error {
	if s.bindAddress == "" || s.bindAddress == "0" {
		s.log.Info("lakeFS auth server disabled")
		<-ctx.Done()
		return nil
	}

	httpServer := &http.Server{
		Addr:              s.bindAddress,
		Handler:           s.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		s.log.Info("starting lakeFS auth server", "bindAddress", s.bindAddress)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handle)
	return mux
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	path := routePath(r.URL.Path)
	if path == "" {
		path = "/"
	}

	switch {
	case r.Method == http.MethodGet && (path == "/healthcheck" || path == "/healthz" || path == "/readyz" || r.URL.Path == "/healthcheck"):
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && path == "/config/version":
		writeJSON(w, http.StatusOK, map[string]string{"version": "dev"})
	case r.Method == http.MethodGet && path == "/auth/users":
		s.listUsers(w, r)
	case r.Method == http.MethodPost && path == "/auth/users":
		s.createUser(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/auth/users/"):
		s.handleUserPath(w, r, strings.TrimPrefix(path, "/auth/users/"))
	case r.Method == http.MethodPost && strings.HasPrefix(path, "/auth/users/") && strings.HasSuffix(path, "/credentials"):
		s.handleUserPath(w, r, strings.TrimPrefix(path, "/auth/users/"))
	case r.Method == http.MethodDelete && strings.HasPrefix(path, "/auth/users/"):
		s.handleUserPath(w, r, strings.TrimPrefix(path, "/auth/users/"))
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/auth/credentials/"):
		s.getCredentialByAccessKey(w, r, strings.TrimPrefix(path, "/auth/credentials/"), "")
	case r.Method == http.MethodGet && path == "/auth/policies":
		s.listPolicies(w, r)
	case strings.HasPrefix(path, "/auth/policies/"):
		s.handlePolicyPath(w, r, strings.TrimPrefix(path, "/auth/policies/"))
	case r.Method == http.MethodPost && path == "/auth/policies":
		s.createPolicy(w, r)
	case r.Method == http.MethodGet && path == "/auth/groups":
		s.listGroups(w, r)
	case r.Method == http.MethodPost && path == "/auth/groups":
		s.createGroup(w, r)
	case strings.HasPrefix(path, "/auth/groups/"):
		s.handleGroupPath(w, r, strings.TrimPrefix(path, "/auth/groups/"))
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) handleUserPath(w http.ResponseWriter, r *http.Request, tail string) {
	parts := strings.Split(strings.Trim(tail, "/"), "/")
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.getUser(w, r, parts[0])
		case http.MethodDelete:
			s.deleteUser(w, r, parts[0])
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	if len(parts) == 2 && parts[1] == "credentials" {
		switch r.Method {
		case http.MethodGet:
			s.listUserCredentials(w, r, parts[0])
		case http.MethodPost:
			s.createUserCredential(w, r, parts[0])
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	if len(parts) == 2 && parts[1] == "policies" && r.Method == http.MethodGet {
		s.listUserPolicies(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "policies" {
		switch r.Method {
		case http.MethodPut:
			s.attachPolicyToUser(w, r, parts[0], parts[2])
		case http.MethodDelete:
			s.detachPolicyFromUser(w, r, parts[0], parts[2])
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	if len(parts) == 2 && parts[1] == "groups" && r.Method == http.MethodGet {
		s.listUserGroups(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "credentials" {
		switch r.Method {
		case http.MethodGet:
			s.getCredentialByAccessKey(w, r, parts[2], parts[0])
		case http.MethodDelete:
			s.revokeUserCredential(w, r, parts[0], parts[2])
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	writeError(w, http.StatusNotFound, "not found")
}

func (s *Server) handlePolicyPath(w http.ResponseWriter, r *http.Request, tail string) {
	parts := strings.Split(strings.Trim(tail, "/"), "/")
	if len(parts) != 1 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.getPolicy(w, r, parts[0])
	case http.MethodPut:
		s.updatePolicy(w, r, parts[0])
	case http.MethodDelete:
		s.deletePolicy(w, r, parts[0])
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleGroupPath(w http.ResponseWriter, r *http.Request, tail string) {
	parts := strings.Split(strings.Trim(tail, "/"), "/")
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.getGroup(w, r, parts[0])
		case http.MethodDelete:
			s.deleteGroup(w, r, parts[0])
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	if len(parts) == 3 && parts[1] == "members" {
		switch r.Method {
		case http.MethodPut:
			s.addGroupMembership(w, r, parts[0], parts[2])
		case http.MethodDelete:
			s.removeGroupMembership(w, r, parts[0], parts[2])
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	if len(parts) == 3 && parts[1] == "policies" {
		switch r.Method {
		case http.MethodPut:
			s.attachPolicyToGroup(w, r, parts[0], parts[2])
		case http.MethodDelete:
			s.detachPolicyFromGroup(w, r, parts[0], parts[2])
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	if len(parts) == 2 && parts[1] == "policies" && r.Method == http.MethodGet {
		s.listGroupPolicies(w, r, parts[0])
		return
	}
	writeError(w, http.StatusNotFound, "not found")
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	users := &pkgv1beta1.LakeFSUserList{}
	if err := s.client.List(r.Context(), users); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list users")
		return
	}

	results := make([]user, 0, len(users.Items))
	for i := range users.Items {
		item := userFromUser(&users.Items[i])
		if !userMatchesListQuery(item, r) {
			continue
		}
		results = append(results, item)
	}
	writeJSON(w, http.StatusOK, newPaginatedResponse(results))
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var request createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid user request")
		return
	}
	username := strings.TrimSpace(firstNonEmpty(request.Username, r.URL.Query().Get("username"), r.URL.Query().Get("user_name")))
	if username == "" {
		writeError(w, http.StatusBadRequest, "username is required")
		return
	}

	externalID := strings.TrimSpace(firstNonEmpty(stringValue(request.ExternalID), stringValue(request.ExternalIDV2), username))
	if user, ok := s.resolveUser(r.Context(), externalID); ok {
		writeJSON(w, http.StatusCreated, userFromUser(user))
		return
	}

	namespace := firstNonEmpty(r.URL.Query().Get("namespace"), s.defaultUserNamespace)
	user := &pkgv1beta1.LakeFSUser{
		ObjectMeta: metav1.ObjectMeta{
			Name:      objectName(username),
			Namespace: namespace,
		},
		Spec: pkgv1beta1.LakeFSUserSpec{
			ExternalID:   externalID,
			FriendlyName: firstNonEmpty(stringValue(request.FriendlyName), username),
		},
	}

	if err := s.client.Create(r.Context(), user); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			writeError(w, http.StatusInternalServerError, "failed to create user")
			return
		}
		if getErr := s.client.Get(r.Context(), types.NamespacedName{Namespace: namespace, Name: user.Name}, user); getErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to read user")
			return
		}
		if userUsername(user) != username && userUsername(user) != externalID {
			writeError(w, http.StatusConflict, "user already exists")
			return
		}
		writeJSON(w, http.StatusCreated, userFromUser(user))
		return
	}

	writeJSON(w, http.StatusCreated, userFromUser(user))
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request, userID string) {
	user, ok := s.resolveUser(r.Context(), userID)
	if !ok {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, userFromUser(user))
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request, userID string) {
	user, ok := s.resolveUser(r.Context(), userID)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.client.Delete(r.Context(), user); err != nil && !apierrors.IsNotFound(err) {
		writeError(w, http.StatusInternalServerError, "failed to delete user")
		return
	}
	s.deleteCredentialsForUser(r.Context(), user)
	s.removeUserFromGroups(r.Context(), user)
	s.deleteRoleBindingsForSubject(r.Context(), user.Namespace, pkgv1beta1.LakeFSRoleBindingSubjectKindUser, user.Name)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteCredentialsForUser(ctx context.Context, user *pkgv1beta1.LakeFSUser) {
	credentials := &pkgv1beta1.LakeFSCredentialList{}
	if err := s.client.List(ctx, credentials, client.InNamespace(user.Namespace), client.MatchingFields{pkgv1beta1.CredentialUserField: user.Name}); err != nil {
		return
	}
	for i := range credentials.Items {
		_ = s.client.Delete(ctx, &credentials.Items[i])
	}
}

func (s *Server) listUserCredentials(w http.ResponseWriter, r *http.Request, userID string) {
	user, ok := s.resolveUser(r.Context(), userID)
	if !ok {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	credentials := &pkgv1beta1.LakeFSCredentialList{}
	if err := s.client.List(
		r.Context(),
		credentials,
		client.InNamespace(user.Namespace),
		client.MatchingFields{pkgv1beta1.CredentialUserField: user.Name},
	); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list credentials")
		return
	}

	results := make([]credential, 0, len(credentials.Items))
	for i := range credentials.Items {
		item := credentials.Items[i]
		if item.Spec.Revoked || credentialExpired(&item) {
			continue
		}
		results = append(results, credential{
			AccessKeyID:  item.Spec.AccessKeyID,
			CreationDate: item.CreationTimestamp.Unix(),
		})
	}
	writeJSON(w, http.StatusOK, newPaginatedResponse(results))
}

func (s *Server) attachPolicyToUser(w http.ResponseWriter, r *http.Request, userID, policyID string) {
	user, ok := s.resolveUser(r.Context(), userID)
	if !ok {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	repository := repositoryScope(r)
	role, ok := s.resolveRoleForPolicy(r.Context(), policyID, repository)
	if !ok {
		writeError(w, http.StatusNotFound, "policy not found")
		return
	}
	if err := s.ensureRoleBinding(r.Context(), user.Namespace, pkgv1beta1.LakeFSRoleBindingSubjectKindUser, user.Name, role.Name, repository); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to attach policy")
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) detachPolicyFromUser(w http.ResponseWriter, r *http.Request, userID, policyID string) {
	user, ok := s.resolveUser(r.Context(), userID)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	repository := repositoryScope(r)
	role, ok := s.resolveRoleForPolicy(r.Context(), policyID, repository)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.deleteRoleBinding(r.Context(), user.Namespace, pkgv1beta1.LakeFSRoleBindingSubjectKindUser, user.Name, role.Name, repository)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listUserGroups(w http.ResponseWriter, r *http.Request, userID string) {
	user, ok := s.resolveUser(r.Context(), userID)
	if !ok {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	groups, err := s.groupsForUser(r.Context(), user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list groups")
		return
	}
	results := make([]group, 0, len(groups))
	for i := range groups {
		results = append(results, groupFromGroup(&groups[i]))
	}
	writeJSON(w, http.StatusOK, newPaginatedResponse(results))
}

func (s *Server) listUserPolicies(w http.ResponseWriter, r *http.Request, userID string) {
	user, ok := s.resolveUser(r.Context(), userID)
	if !ok {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	policies, err := s.effectivePoliciesForUser(r.Context(), user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list policies")
		return
	}
	writeJSON(w, http.StatusOK, newPaginatedResponse(policies))
}

func (s *Server) listGroups(w http.ResponseWriter, r *http.Request) {
	groups := &pkgv1beta1.LakeFSGroupList{}
	if err := s.client.List(r.Context(), groups); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list groups")
		return
	}

	results := make([]group, 0, len(builtinGroups)+len(groups.Items))
	for groupID := range builtinGroups {
		if item, ok := builtinGroup(groupID); ok {
			results = append(results, item)
		}
	}
	for i := range groups.Items {
		results = append(results, groupFromGroup(&groups.Items[i]))
	}
	writeJSON(w, http.StatusOK, newPaginatedResponse(results))
}

func (s *Server) createGroup(w http.ResponseWriter, r *http.Request) {
	var request createGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid group request")
		return
	}
	groupID := strings.TrimSpace(firstNonEmpty(request.ID, request.Name))
	if groupID == "" {
		writeError(w, http.StatusBadRequest, "group id is required")
		return
	}
	if item, ok := s.resolveGroup(r.Context(), groupID); ok {
		writeJSON(w, http.StatusCreated, groupFromGroup(item))
		return
	}

	group, err := s.ensureGroup(r.Context(), groupID, firstNonEmpty(request.Description, request.Name, groupID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create group")
		return
	}
	writeJSON(w, http.StatusCreated, groupFromGroup(group))
}

func (s *Server) getGroup(w http.ResponseWriter, r *http.Request, groupID string) {
	if group, ok := s.resolveGroup(r.Context(), groupID); ok {
		writeJSON(w, http.StatusOK, groupFromGroup(group))
		return
	}
	if item, ok := builtinGroup(groupID); ok {
		writeJSON(w, http.StatusOK, item)
		return
	}
	writeError(w, http.StatusNotFound, "group not found")
}

func (s *Server) deleteGroup(w http.ResponseWriter, r *http.Request, groupID string) {
	group, ok := s.resolveGroup(r.Context(), groupID)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.client.Delete(r.Context(), group); err != nil && !apierrors.IsNotFound(err) {
		writeError(w, http.StatusInternalServerError, "failed to delete group")
		return
	}
	s.deleteRoleBindingsForSubject(r.Context(), group.Namespace, pkgv1beta1.LakeFSRoleBindingSubjectKindGroup, group.Name)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) addGroupMembership(w http.ResponseWriter, r *http.Request, groupID, userID string) {
	group, ok := s.resolveGroup(r.Context(), groupID)
	if !ok {
		if _, builtin := builtinGroup(groupID); !builtin {
			writeError(w, http.StatusNotFound, "group not found")
			return
		}
		var err error
		group, err = s.ensureGroup(r.Context(), groupID, groupID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create group")
			return
		}
	}
	user, ok := s.resolveUser(r.Context(), userID)
	if !ok || user.Namespace != group.Namespace {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	for _, member := range group.Spec.Users {
		if member.Name == user.Name {
			w.WriteHeader(http.StatusCreated)
			return
		}
	}
	group.Spec.Users = append(group.Spec.Users, pkgv1beta1.LakeFSUserReference{Name: user.Name})
	if err := s.client.Update(r.Context(), group); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add group member")
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) removeGroupMembership(w http.ResponseWriter, r *http.Request, groupID, userID string) {
	group, ok := s.resolveGroup(r.Context(), groupID)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	user, ok := s.resolveUser(r.Context(), userID)
	if !ok || user.Namespace != group.Namespace {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	members := group.Spec.Users[:0]
	for _, member := range group.Spec.Users {
		if member.Name != user.Name {
			members = append(members, member)
		}
	}
	group.Spec.Users = members
	if err := s.client.Update(r.Context(), group); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove group member")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) attachPolicyToGroup(w http.ResponseWriter, r *http.Request, groupID, policyID string) {
	group, ok := s.resolveGroup(r.Context(), groupID)
	if !ok {
		if _, builtin := builtinGroup(groupID); !builtin {
			writeError(w, http.StatusNotFound, "group not found")
			return
		}
		var err error
		group, err = s.ensureGroup(r.Context(), groupID, groupID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create group")
			return
		}
	}
	repository := repositoryScope(r)
	role, ok := s.resolveRoleForPolicy(r.Context(), policyID, repository)
	if !ok {
		writeError(w, http.StatusNotFound, "policy not found")
		return
	}
	if err := s.ensureRoleBinding(r.Context(), group.Namespace, pkgv1beta1.LakeFSRoleBindingSubjectKindGroup, group.Name, role.Name, repository); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to attach policy")
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) detachPolicyFromGroup(w http.ResponseWriter, r *http.Request, groupID, policyID string) {
	group, ok := s.resolveGroup(r.Context(), groupID)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	repository := repositoryScope(r)
	role, ok := s.resolveRoleForPolicy(r.Context(), policyID, repository)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.deleteRoleBinding(r.Context(), group.Namespace, pkgv1beta1.LakeFSRoleBindingSubjectKindGroup, group.Name, role.Name, repository)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listGroupPolicies(w http.ResponseWriter, r *http.Request, groupID string) {
	group, ok := s.resolveGroup(r.Context(), groupID)
	if !ok {
		writeError(w, http.StatusNotFound, "group not found")
		return
	}
	policies, err := s.effectivePoliciesForSubject(r.Context(), group.Namespace, pkgv1beta1.LakeFSRoleBindingSubjectKindGroup, group.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list policies")
		return
	}
	writeJSON(w, http.StatusOK, newPaginatedResponse(policies))
}

func (s *Server) listPolicies(w http.ResponseWriter, r *http.Request) {
	roles := &pkgv1beta1.LakeFSRoleList{}
	if err := s.client.List(r.Context(), roles); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list policies")
		return
	}

	results := make([]policy, 0)
	for i := range roles.Items {
		results = append(results, policiesFromRole(&roles.Items[i], "*")...)
	}
	writeJSON(w, http.StatusOK, newPaginatedResponse(results))
}

func (s *Server) createPolicy(w http.ResponseWriter, r *http.Request) {
	s.writePolicy(w, r, http.StatusCreated)
}

func (s *Server) updatePolicy(w http.ResponseWriter, r *http.Request, policyID string) {
	s.writePolicy(w, r, http.StatusOK, policyID)
}

func (s *Server) writePolicy(w http.ResponseWriter, r *http.Request, status int, fallbackName ...string) {
	var request policy
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid policy request")
		return
	}
	if request.Name == "" && len(fallbackName) > 0 {
		request.Name = fallbackName[0]
	}
	if request.Name == "" {
		writeError(w, http.StatusBadRequest, "policy name is required")
		return
	}

	role := roleFromPolicy(s.defaultUserNamespace, request)
	err := s.client.Create(r.Context(), role)
	if apierrors.IsAlreadyExists(err) {
		existing := &pkgv1beta1.LakeFSRole{}
		key := types.NamespacedName{Namespace: role.Namespace, Name: role.Name}
		if getErr := s.client.Get(r.Context(), key, existing); getErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to read policy")
			return
		}
		existing.Spec = role.Spec
		if updateErr := s.client.Update(r.Context(), existing); updateErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to update policy")
			return
		}
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create policy")
		return
	}
	writeJSON(w, status, request)
}

func (s *Server) deletePolicy(w http.ResponseWriter, r *http.Request, policyID string) {
	role, ok := s.resolveRoleForPolicy(r.Context(), policyID, "*")
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.client.Delete(r.Context(), role); err != nil && !apierrors.IsNotFound(err) {
		writeError(w, http.StatusInternalServerError, "failed to delete policy")
		return
	}
	s.deleteRoleBindingsForRole(r.Context(), role.Namespace, role.Name)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getPolicy(w http.ResponseWriter, r *http.Request, policyID string) {
	item, ok := s.resolvePolicy(r.Context(), policyID)
	if ok {
		writeJSON(w, http.StatusOK, item)
		return
	}
	writeError(w, http.StatusNotFound, "policy not found")
}

func (s *Server) getCredentialByAccessKey(w http.ResponseWriter, r *http.Request, accessKeyID, expectedUserID string) {
	credentialObject, ok := s.resolveCredential(r.Context(), accessKeyID)
	if !ok || credentialObject.Spec.Revoked || credentialExpired(credentialObject) {
		writeError(w, http.StatusNotFound, "credential not found")
		return
	}

	user := &pkgv1beta1.LakeFSUser{}
	userKey := types.NamespacedName{Namespace: credentialObject.Namespace, Name: credentialObject.Spec.UserRef.Name}
	if err := s.client.Get(r.Context(), userKey, user); err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if expectedUserID != "" && userUsername(user) != expectedUserID {
		writeError(w, http.StatusNotFound, "credential not found")
		return
	}

	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{Namespace: credentialObject.Namespace, Name: credentialObject.Spec.SecretRef.Name}
	if err := s.client.Get(r.Context(), secretKey, secret); err != nil {
		writeError(w, http.StatusNotFound, "secret not found")
		return
	}
	secretAccessKey := string(secret.Data[credentialObject.Spec.SecretKey()])
	if secretAccessKey == "" {
		writeError(w, http.StatusNotFound, "credential secret not found")
		return
	}

	writeJSON(w, http.StatusOK, credentialsWithSecret{
		AccessKeyID:     credentialObject.Spec.AccessKeyID,
		SecretAccessKey: secretAccessKey,
		CreationDate:    credentialObject.CreationTimestamp.Unix(),
		UserID:          compatibilityUserID(userUsername(user)),
		UserName:        userUsername(user),
	})
}

func (s *Server) createUserCredential(w http.ResponseWriter, r *http.Request, userID string) {
	user, ok := s.resolveUser(r.Context(), userID)
	if !ok {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	accessKeyID := firstNonEmpty(r.URL.Query().Get("access_key_id"), r.URL.Query().Get("access_key"))
	if accessKeyID == "" {
		accessKeyID = "lakefs_ak_" + randomToken(18)
	}
	secretAccessKey := firstNonEmpty(r.URL.Query().Get("secret_access_key"), r.URL.Query().Get("secret_key"))

	name := objectName("lakefs-cred-" + accessKeyID)
	credentialObject := &pkgv1beta1.LakeFSCredential{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: user.Namespace},
		Spec: pkgv1beta1.LakeFSCredentialSpec{
			UserRef:     pkgv1beta1.LakeFSUserReference{Name: user.Name},
			AccessKeyID: accessKeyID,
			SecretRef:   pkgv1beta1.LakeFSSecretReference{Name: name, Key: "secretAccessKey"},
		},
	}
	err := s.client.Create(r.Context(), credentialObject)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		writeError(w, http.StatusInternalServerError, "failed to create credential")
		return
	}
	if apierrors.IsAlreadyExists(err) {
		if getErr := s.client.Get(r.Context(), types.NamespacedName{Namespace: user.Namespace, Name: name}, credentialObject); getErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to read credential")
			return
		}
	}

	if secretAccessKey != "" {
		if err := s.ensureCredentialSecret(r.Context(), credentialObject, secretAccessKey); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create credential secret")
			return
		}
		writeJSON(w, http.StatusCreated, credentialsWithSecret{
			AccessKeyID:     accessKeyID,
			SecretAccessKey: secretAccessKey,
			CreationDate:    time.Now().Unix(),
			UserID:          compatibilityUserID(userUsername(user)),
			UserName:        userUsername(user),
		})
		return
	}

	generatedSecret, ok := s.waitForCredentialSecret(r.Context(), credentialObject, 10*time.Second)
	if ok {
		writeJSON(w, http.StatusCreated, credentialsWithSecret{
			AccessKeyID:     accessKeyID,
			SecretAccessKey: generatedSecret,
			CreationDate:    time.Now().Unix(),
			UserID:          compatibilityUserID(userUsername(user)),
			UserName:        userUsername(user),
		})
		return
	} else {
		writeJSON(w, http.StatusAccepted, credential{
			AccessKeyID:  accessKeyID,
			CreationDate: time.Now().Unix(),
		})
		return
	}
}

func (s *Server) ensureCredentialSecret(ctx context.Context, credentialObject *pkgv1beta1.LakeFSCredential, secretAccessKey string) error {
	secretKey := types.NamespacedName{Namespace: credentialObject.Namespace, Name: credentialObject.Spec.SecretRef.Name}
	secret := &corev1.Secret{}
	if err := s.client.Get(ctx, secretKey, secret); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretKey.Name,
				Namespace: secretKey.Namespace,
				Labels: map[string]string{
					"lakefs.versioneer.at/credential": "true",
					"lakefs.versioneer.at/user":       credentialObject.Spec.UserRef.Name,
				},
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"accessKeyId":                     []byte(credentialObject.Spec.AccessKeyID),
				credentialObject.Spec.SecretKey(): []byte(secretAccessKey),
			},
		}
		return s.client.Create(ctx, secret)
	}

	changed := false
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
		changed = true
	}
	if string(secret.Data["accessKeyId"]) != credentialObject.Spec.AccessKeyID {
		secret.Data["accessKeyId"] = []byte(credentialObject.Spec.AccessKeyID)
		changed = true
	}
	if string(secret.Data[credentialObject.Spec.SecretKey()]) != secretAccessKey {
		secret.Data[credentialObject.Spec.SecretKey()] = []byte(secretAccessKey)
		changed = true
	}
	if secret.Labels == nil {
		secret.Labels = map[string]string{}
		changed = true
	}
	if secret.Labels["lakefs.versioneer.at/credential"] != "true" {
		secret.Labels["lakefs.versioneer.at/credential"] = "true"
		changed = true
	}
	if secret.Labels["lakefs.versioneer.at/user"] != credentialObject.Spec.UserRef.Name {
		secret.Labels["lakefs.versioneer.at/user"] = credentialObject.Spec.UserRef.Name
		changed = true
	}
	if !changed {
		return nil
	}
	return s.client.Update(ctx, secret)
}

func (s *Server) waitForCredentialSecret(ctx context.Context, credentialObject *pkgv1beta1.LakeFSCredential, timeout time.Duration) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		secretAccessKey, ok := s.getCredentialSecret(ctx, credentialObject)
		if ok {
			return secretAccessKey, true
		}
		select {
		case <-ctx.Done():
			return "", false
		case <-ticker.C:
		}
	}
}

func (s *Server) getCredentialSecret(ctx context.Context, credentialObject *pkgv1beta1.LakeFSCredential) (string, bool) {
	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{Namespace: credentialObject.Namespace, Name: credentialObject.Spec.SecretRef.Name}
	if err := s.client.Get(ctx, secretKey, secret); err != nil {
		return "", false
	}
	secretAccessKey := string(secret.Data[credentialObject.Spec.SecretKey()])
	return secretAccessKey, secretAccessKey != ""
}

func (s *Server) revokeUserCredential(w http.ResponseWriter, r *http.Request, userID, accessKeyID string) {
	credentialObject, ok := s.resolveCredential(r.Context(), accessKeyID)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	user, userOK := s.resolveUser(r.Context(), userID)
	if !userOK || user.Namespace != credentialObject.Namespace || user.Name != credentialObject.Spec.UserRef.Name {
		writeError(w, http.StatusNotFound, "credential not found")
		return
	}
	credentialObject.Spec.Revoked = true
	if err := s.client.Update(r.Context(), credentialObject); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke credential")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) resolveCredential(ctx context.Context, accessKeyID string) (*pkgv1beta1.LakeFSCredential, bool) {
	credentials := &pkgv1beta1.LakeFSCredentialList{}
	if err := s.client.List(ctx, credentials, client.MatchingFields{pkgv1beta1.CredentialAccessKeyIDField: accessKeyID}); err != nil {
		return nil, false
	}
	if len(credentials.Items) == 0 {
		return nil, false
	}
	return &credentials.Items[0], true
}

func (s *Server) resolveUser(ctx context.Context, userID string) (*pkgv1beta1.LakeFSUser, bool) {
	users := &pkgv1beta1.LakeFSUserList{}
	if err := s.client.List(ctx, users, client.MatchingFields{pkgv1beta1.UserExternalIDField: userID}); err == nil {
		for i := range users.Items {
			if userUsername(&users.Items[i]) == userID {
				return &users.Items[i], true
			}
		}
	}
	if strings.Contains(userID, ":") {
		parts := strings.SplitN(userID, ":", 2)
		user := &pkgv1beta1.LakeFSUser{}
		if err := s.client.Get(ctx, types.NamespacedName{Namespace: parts[0], Name: parts[1]}, user); err == nil {
			return user, true
		}
	}
	if err := s.client.List(ctx, users); err != nil {
		return nil, false
	}
	for i := range users.Items {
		if userUsername(&users.Items[i]) == userID || users.Items[i].Name == userID {
			return &users.Items[i], true
		}
	}
	return nil, false
}

func (s *Server) resolveGroup(ctx context.Context, groupID string) (*pkgv1beta1.LakeFSGroup, bool) {
	groups := &pkgv1beta1.LakeFSGroupList{}
	if err := s.client.List(ctx, groups, client.MatchingFields{pkgv1beta1.GroupExternalIDField: groupID}); err == nil {
		for i := range groups.Items {
			if groupName(&groups.Items[i]) == groupID {
				return &groups.Items[i], true
			}
		}
	}
	if strings.Contains(groupID, ":") {
		parts := strings.SplitN(groupID, ":", 2)
		group := &pkgv1beta1.LakeFSGroup{}
		if err := s.client.Get(ctx, types.NamespacedName{Namespace: parts[0], Name: parts[1]}, group); err == nil {
			return group, true
		}
	}
	if err := s.client.List(ctx, groups); err != nil {
		return nil, false
	}
	for i := range groups.Items {
		if groupName(&groups.Items[i]) == groupID || groups.Items[i].Name == groupID || groups.Items[i].Name == objectName("group-"+groupID) {
			return &groups.Items[i], true
		}
	}
	return nil, false
}

func (s *Server) ensureGroup(ctx context.Context, groupID, description string) (*pkgv1beta1.LakeFSGroup, error) {
	if group, ok := s.resolveGroup(ctx, groupID); ok {
		return group, nil
	}
	group := &pkgv1beta1.LakeFSGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      objectName("group-" + groupID),
			Namespace: s.defaultUserNamespace,
		},
		Spec: pkgv1beta1.LakeFSGroupSpec{
			ExternalID:  groupID,
			Description: firstNonEmpty(description, groupID),
		},
	}
	if err := s.client.Create(ctx, group); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return nil, err
		}
		if getErr := s.client.Get(ctx, types.NamespacedName{Namespace: group.Namespace, Name: group.Name}, group); getErr != nil {
			return nil, getErr
		}
	}
	return group, nil
}

func (s *Server) resolvePolicy(ctx context.Context, policyID string) (policy, bool) {
	role, ok := s.resolveRoleForPolicy(ctx, policyID, "*")
	if !ok {
		return policy{}, false
	}
	for _, item := range policiesFromRole(role, "*") {
		if item.Name == policyID {
			return item, true
		}
	}
	return policy{}, false
}

func (s *Server) resolveRoleForPolicy(ctx context.Context, policyID, repository string) (*pkgv1beta1.LakeFSRole, bool) {
	roles := &pkgv1beta1.LakeFSRoleList{}
	if err := s.client.List(ctx, roles); err != nil {
		return nil, false
	}
	for i := range roles.Items {
		for _, item := range policiesFromRole(&roles.Items[i], repository) {
			if item.Name == policyID {
				return &roles.Items[i], true
			}
		}
	}
	return nil, false
}

func (s *Server) groupsForUser(ctx context.Context, user *pkgv1beta1.LakeFSUser) ([]pkgv1beta1.LakeFSGroup, error) {
	groups := &pkgv1beta1.LakeFSGroupList{}
	if err := s.client.List(ctx, groups, client.InNamespace(user.Namespace)); err != nil {
		return nil, err
	}
	results := make([]pkgv1beta1.LakeFSGroup, 0)
	for i := range groups.Items {
		for _, member := range groups.Items[i].Spec.Users {
			if member.Name == user.Name {
				results = append(results, groups.Items[i])
				break
			}
		}
	}
	return results, nil
}

func (s *Server) effectivePoliciesForUser(ctx context.Context, user *pkgv1beta1.LakeFSUser) ([]policy, error) {
	results, err := s.effectivePoliciesForSubject(ctx, user.Namespace, pkgv1beta1.LakeFSRoleBindingSubjectKindUser, user.Name)
	if err != nil {
		return nil, err
	}

	groups, err := s.groupsForUser(ctx, user)
	if err != nil {
		return nil, err
	}
	for i := range groups {
		groupPolicies, err := s.effectivePoliciesForSubject(ctx, user.Namespace, pkgv1beta1.LakeFSRoleBindingSubjectKindGroup, groups[i].Name)
		if err != nil {
			return nil, err
		}
		results = append(results, groupPolicies...)
	}
	return results, nil
}

func (s *Server) effectivePoliciesForSubject(ctx context.Context, namespace string, kind pkgv1beta1.LakeFSRoleBindingSubjectKind, name string) ([]policy, error) {
	bindings := &pkgv1beta1.LakeFSRoleBindingList{}
	if err := s.client.List(ctx, bindings, client.InNamespace(namespace)); err != nil {
		return nil, err
	}

	results := make([]policy, 0)
	for i := range bindings.Items {
		binding := &bindings.Items[i]
		if binding.Spec.Subject.Kind != kind || binding.Spec.Subject.Name != name {
			continue
		}
		role := &pkgv1beta1.LakeFSRole{}
		key := types.NamespacedName{Namespace: namespace, Name: binding.Spec.RoleRef.Name}
		if err := s.client.Get(ctx, key, role); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		results = append(results, policiesFromRole(role, binding.Spec.Repository)...)
	}
	return results, nil
}

func (s *Server) ensureRoleBinding(ctx context.Context, namespace string, kind pkgv1beta1.LakeFSRoleBindingSubjectKind, subjectName, roleName, repository string) error {
	name := roleBindingName(kind, subjectName, roleName, repository)
	binding := &pkgv1beta1.LakeFSRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: pkgv1beta1.LakeFSRoleBindingSpec{
			Subject:    pkgv1beta1.LakeFSRoleBindingSubject{Kind: kind, Name: subjectName},
			RoleRef:    pkgv1beta1.LakeFSRoleReference{Name: roleName},
			Repository: repository,
		},
	}
	if err := s.client.Create(ctx, binding); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
		existing := &pkgv1beta1.LakeFSRoleBinding{}
		key := types.NamespacedName{Namespace: namespace, Name: name}
		if getErr := s.client.Get(ctx, key, existing); getErr != nil {
			return getErr
		}
		existing.Spec = binding.Spec
		return s.client.Update(ctx, existing)
	}
	return nil
}

func (s *Server) deleteRoleBinding(ctx context.Context, namespace string, kind pkgv1beta1.LakeFSRoleBindingSubjectKind, subjectName, roleName, repository string) {
	binding := &pkgv1beta1.LakeFSRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleBindingName(kind, subjectName, roleName, repository),
			Namespace: namespace,
		},
	}
	_ = s.client.Delete(ctx, binding)
}

func (s *Server) deleteRoleBindingsForSubject(ctx context.Context, namespace string, kind pkgv1beta1.LakeFSRoleBindingSubjectKind, subjectName string) {
	bindings := &pkgv1beta1.LakeFSRoleBindingList{}
	if err := s.client.List(ctx, bindings, client.InNamespace(namespace)); err != nil {
		return
	}
	for i := range bindings.Items {
		if bindings.Items[i].Spec.Subject.Kind == kind && bindings.Items[i].Spec.Subject.Name == subjectName {
			_ = s.client.Delete(ctx, &bindings.Items[i])
		}
	}
}

func (s *Server) deleteRoleBindingsForRole(ctx context.Context, namespace, roleName string) {
	bindings := &pkgv1beta1.LakeFSRoleBindingList{}
	if err := s.client.List(ctx, bindings, client.InNamespace(namespace)); err != nil {
		return
	}
	for i := range bindings.Items {
		if bindings.Items[i].Spec.RoleRef.Name == roleName {
			_ = s.client.Delete(ctx, &bindings.Items[i])
		}
	}
}

func (s *Server) removeUserFromGroups(ctx context.Context, user *pkgv1beta1.LakeFSUser) {
	groups, err := s.groupsForUser(ctx, user)
	if err != nil {
		return
	}
	for i := range groups {
		members := groups[i].Spec.Users[:0]
		for _, member := range groups[i].Spec.Users {
			if member.Name != user.Name {
				members = append(members, member)
			}
		}
		groups[i].Spec.Users = members
		_ = s.client.Update(ctx, &groups[i])
	}
}

func roleBindingName(kind pkgv1beta1.LakeFSRoleBindingSubjectKind, subjectName, roleName, repository string) string {
	return objectName(fmt.Sprintf("lakefs-rb-%s-%s-%s-%s", strings.ToLower(string(kind)), subjectName, roleName, repository))
}

func repositoryScope(r *http.Request) string {
	return firstNonEmpty(r.URL.Query().Get("repository"), r.URL.Query().Get("repo"), "*")
}

func routePath(path string) string {
	for _, prefix := range []string{"/auth/v1", "/api/v1"} {
		if path == prefix {
			return "/"
		}
		if strings.HasPrefix(path, prefix+"/") {
			return strings.TrimPrefix(path, prefix)
		}
	}
	return path
}

func newPaginatedResponse[T any](results []T) paginatedResponse[T] {
	return paginatedResponse[T]{
		Pagination: pagination{
			HasMore:    false,
			Results:    len(results),
			MaxPerPage: 100,
		},
		Results: results,
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Message: message})
}

func userFromUser(item *pkgv1beta1.LakeFSUser) user {
	return user{
		Username:     userUsername(item),
		CreationDate: item.CreationTimestamp.Unix(),
		FriendlyName: item.Spec.FriendlyName,
		Email:        item.Spec.ExternalID,
		ExternalID:   item.Spec.ExternalID,
	}
}

func groupFromGroup(item *pkgv1beta1.LakeFSGroup) group {
	name := groupName(item)
	return group{
		ID:           name,
		Name:         name,
		CreationDate: item.CreationTimestamp.Unix(),
		Description:  item.Spec.Description,
	}
}

func builtinGroup(groupID string) (group, bool) {
	if _, ok := builtinGroups[groupID]; !ok {
		return group{}, false
	}
	return group{
		ID:           groupID,
		Name:         groupID,
		CreationDate: time.Now().Unix(),
		Description:  groupID,
	}, true
}

func groupName(item *pkgv1beta1.LakeFSGroup) string {
	if item.Spec.ExternalID != "" {
		return item.Spec.ExternalID
	}
	if item.Namespace == "" {
		return item.Name
	}
	return item.Namespace + ":" + item.Name
}

func roleFromPolicy(namespace string, item policy) *pkgv1beta1.LakeFSRole {
	statements := make([]pkgv1beta1.LakeFSPolicyStatement, 0, len(item.Statement))
	for _, statement := range item.Statement {
		statements = append(statements, pkgv1beta1.LakeFSPolicyStatement{
			Effect:   statement.Effect,
			Resource: statement.Resource,
			Action:   statement.Action,
		})
	}
	return &pkgv1beta1.LakeFSRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      objectName("lakefs-policy-" + item.Name),
			Namespace: namespace,
		},
		Spec: pkgv1beta1.LakeFSRoleSpec{
			Description: "lakeFS setup policy " + item.Name,
			Policies: []pkgv1beta1.LakeFSPolicyTemplate{
				{
					Name:      item.Name,
					Statement: statements,
				},
			},
		},
	}
}

func userMatchesListQuery(item user, r *http.Request) bool {
	query := r.URL.Query()
	if id := query.Get("id"); id != "" && fmt.Sprint(compatibilityUserID(item.Username)) != id {
		return false
	}
	if email := query.Get("email"); email != "" && item.Email != email {
		return false
	}
	if externalID := firstNonEmpty(query.Get("external_id"), query.Get("externalId")); externalID != "" && item.ExternalID != externalID {
		return false
	}
	if prefix := query.Get("prefix"); prefix != "" && !strings.HasPrefix(item.Username, prefix) {
		return false
	}
	return true
}

func policiesFromRole(role *pkgv1beta1.LakeFSRole, repository string) []policy {
	results := make([]policy, 0, len(role.Spec.Policies))
	for _, template := range role.Spec.Policies {
		item := policy{
			Name:         renderRepositoryTemplate(template.Name, repository),
			CreationDate: role.CreationTimestamp.Unix(),
			Statement:    make([]statement, 0, len(template.Statement)),
		}
		for _, roleStatement := range template.Statement {
			item.Statement = append(item.Statement, statement{
				Effect:   roleStatement.Effect,
				Resource: renderRepositoryTemplate(roleStatement.Resource, repository),
				Action:   roleStatement.Action,
			})
		}
		results = append(results, item)
	}
	return results
}

func renderRepositoryTemplate(value, repository string) string {
	if repository == "" {
		repository = "*"
	}
	return strings.ReplaceAll(value, "<REPOSITORY>", repository)
}

func userUsername(user *pkgv1beta1.LakeFSUser) string {
	if user.Spec.ExternalID != "" {
		return user.Spec.ExternalID
	}
	if user.Namespace == "" {
		return user.Name
	}
	return user.Namespace + ":" + user.Name
}

func credentialExpired(credential *pkgv1beta1.LakeFSCredential) bool {
	return credential.Spec.ExpiresAt != nil && !credential.Spec.ExpiresAt.Time.After(time.Now())
}

func compatibilityUserID(username string) int64 {
	var hash uint64 = 14695981039346656037
	for _, c := range []byte(username) {
		hash ^= uint64(c)
		hash *= 1099511628211
	}
	return int64(hash & 0x7fffffffffffffff)
}

func randomToken(length int) string {
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("failed to read random bytes: %v", err))
	}
	return strings.TrimRight(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), "=")
}

func objectName(value string) string {
	name := strings.ToLower(value)
	name = safeObjectName.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-.")
	if len(name) > 63 {
		name = name[:63]
		name = strings.Trim(name, "-.")
	}
	if name == "" {
		return "lakefs-resource"
	}
	return name
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
