//
// Last.Backend LLC CONFIDENTIAL
// __________________
//
// [2014] - [2017] Last.Backend LLC
// All Rights Reserved.
//
// NOTICE:  All information contained herein is, and remains
// the property of Last.Backend LLC and its suppliers,
// if any.  The intellectual and technical concepts contained
// herein are proprietary to Last.Backend LLC
// and its suppliers and may be covered by Russian Federation and Foreign Patents,
// patents in process, and are protected by trade secret or copyright law.
// Dissemination of this information or reproduction of this material
// is strictly forbidden unless prior written permission is obtained
// from Last.Backend LLC.
//

package routes

import (
	"github.com/gorilla/mux"
	"github.com/lastbackend/lastbackend/pkg/apis/types"
	"github.com/lastbackend/lastbackend/pkg/daemon/config"
	c "github.com/lastbackend/lastbackend/pkg/daemon/context"
	"github.com/lastbackend/lastbackend/pkg/errors"
	"github.com/lastbackend/lastbackend/pkg/vendors"
	"github.com/lastbackend/lastbackend/pkg/vendors/interfaces"
	"net/http"
)

// Авторизация сторонних сервисов для платформы
func OAuthConnectH(w http.ResponseWriter, r *http.Request) {

	var (
		clientID       string
		clientSecretID string
		redirectURI    string
		client         interfaces.IAuth
		session        *types.Session
		ctx            = c.Get()
		params         = mux.Vars(r)
		vendor         = params["vendor"]
		code           = params["code"]
	)

	ctx.Log.Debug("Connect service handler")

	s := r.Context().Value(`session`)
	if s == nil {
		ctx.Log.Error("Error: get session context")
		errors.New("user").Unauthorized().Http(w)
		return
	}

	session = s.(*types.Session)

	clientID, clientSecretID, redirectURI = config.Get().GetVendorConfig(vendor)

	if clientID == "" || clientSecretID == "" {
		ctx.Log.Error("Error: user unauthorized")
		errors.HTTP.Unauthorized(w)
		return
	}

	// Get client for github/bitbucket/gitlab (or anything if implement adapter.OAuth interface) by vendor in user or organization mode
	switch vendor {
	case "github":
		client = vendors.GetGitHub(clientID, clientSecretID, redirectURI)
	case "bitbucket":
		client = vendors.GetBitBucket(clientID, clientSecretID, redirectURI)
	case "gitlab":
		client = vendors.GetGitLab(clientID, clientSecretID, redirectURI)
	default:
		ctx.Log.Error("vendor is not supported yet")
		errors.BadParameter("vendor").Http(w)
		return
	}

	token, err := client.GetToken(code)
	if err != nil {
		ctx.Log.Error(err)
		errors.HTTP.InternalServerError(w)
		return
	}

	serviceUser, err := client.GetUser(token)
	if err != nil {
		ctx.Log.Error(err)
		errors.HTTP.InternalServerError(w)
		return
	}

	vendorInfo := client.GetVendorInfo()

	if err := ctx.Storage.Vendor().Insert(session.Username, serviceUser.Username, vendorInfo.Vendor, vendorInfo.Host, serviceUser.ServiceID, token); err != nil {
		ctx.Log.Error(err.Error())
		errors.HTTP.InternalServerError(w)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte{})
}

func OAuthDisconnectH(w http.ResponseWriter, r *http.Request) {

	var (
		ctx     = c.Get()
		session *types.Session
		params  = mux.Vars(r)
		vendor  = params[`vendor`]
	)

	ctx.Log.Debug("Disconnect service handler")

	s := r.Context().Value(`session`)
	if s == nil {
		ctx.Log.Error("Error: get session context")
		errors.New("user").Unauthorized().Http(w)
		return
	}

	session = s.(*types.Session)

	userModel, err := ctx.Storage.User().GetByUsername(session.Username)
	if err != nil {
		ctx.Log.Error(err)
		errors.HTTP.InternalServerError(w)
		return
	}

	if err := ctx.Storage.Vendor().Remove(userModel.Username, vendor); err != nil {
		ctx.Log.Error(err)
		errors.HTTP.InternalServerError(w)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte{})
}

func VCSRepositoriesListH(w http.ResponseWriter, r *http.Request) {

	var (
		clientID, clientSecretID, redirectURI string
		client                                interfaces.IVCS
		session                               *types.Session
		ctx                                   = c.Get()
		params                                = mux.Vars(r)
		vendor                                = params[`vendor`]
	)

	s := r.Context().Value(`session`)
	if s == nil {
		ctx.Log.Error("Error: get session context")
		errors.New("user").Unauthorized().Http(w)
		return
	}

	session = s.(*types.Session)

	clientID, clientSecretID, redirectURI = config.Get().GetVendorConfig(vendor)

	if clientID == "" || clientSecretID == "" {
		ctx.Log.Error("Error: user unauthorized")
		errors.HTTP.Unauthorized(w)
		return
	}

	// Get client for github/bitbucket/gitlab (or anything if implement adapter.OAuth interface) by vendor in user or organization mode
	switch vendor {
	case "github":
		client = vendors.GetGitHub(clientID, clientSecretID, redirectURI)
	case "bitbucket":
		client = vendors.GetBitBucket(clientID, clientSecretID, redirectURI)
	case "gitlab":
		client = vendors.GetGitLab(clientID, clientSecretID, redirectURI)
	default:
		ctx.Log.Error("vendor is not supported yet")
		errors.BadParameter("vendor").Http(w)
		return
	}

	// ************************ Update token ************************ //

	vendorInfo := client.GetVendorInfo()

	oaModel, err := ctx.Storage.Vendor().Get(session.Username, vendorInfo.Vendor)
	if err != nil {
		ctx.Log.Error(err)
		errors.HTTP.InternalServerError(w)
	}

	token, modify, err := client.RefreshToken(oaModel.Token)
	if err != nil {
		ctx.Log.Error(err)
		errors.HTTP.InternalServerError(w)
	}

	u, err := client.GetUser(token)
	if err != nil {
		ctx.Log.Error(err)
		errors.HTTP.InternalServerError(w)
	}

	if modify {

		oaModel.Host = vendorInfo.Host
		oaModel.Vendor = vendorInfo.Vendor
		oaModel.ServiceID = u.ServiceID
		oaModel.Token = token
		oaModel.Username = u.Username

		if err = ctx.Storage.Vendor().Update(session.Username, oaModel); err != nil {
			ctx.Log.Error(err)
			errors.HTTP.InternalServerError(w)
		}
	}

	// ************************ End update token ************************ //

	repos, err := client.ListRepositories(token, u.Username, false)
	if err != nil {
		ctx.Log.Error(err)
		errors.HTTP.InternalServerError(w)
		return
	}

	rp := types.VCSRepositoryList{}
	rp.Convert(repos)
	response, err := rp.ToJson()

	w.WriteHeader(200)
	w.Write(response)
}

func VCSBranchesListH(w http.ResponseWriter, r *http.Request) {

	var (
		clientID, clientSecretID, redirectURI string
		client                                interfaces.IVCS
		session                               *types.Session
		ctx                                   = c.Get()
		params                                = mux.Vars(r)
		vendor                                = params[`vendor`]
		repo                                  = r.URL.Query().Get(`repo`)
	)

	s := r.Context().Value(`session`)
	if s == nil {
		ctx.Log.Error("Error: get session context")
		errors.New("user").Unauthorized().Http(w)
		return
	}

	session = s.(*types.Session)

	clientID, clientSecretID, redirectURI = config.Get().GetVendorConfig(vendor)

	if clientID == "" || clientSecretID == "" {
		ctx.Log.Error("Error: user unauthorized")
		errors.HTTP.Unauthorized(w)
		return
	}

	// Get client for github/bitbucket/gitlab (or anything if implement adapter.OAuth interface) by vendor in user or organization mode
	switch vendor {
	case "github":
		client = vendors.GetGitHub(clientID, clientSecretID, redirectURI)
	case "bitbucket":
		client = vendors.GetBitBucket(clientID, clientSecretID, redirectURI)
	case "gitlab":
		client = vendors.GetGitLab(clientID, clientSecretID, redirectURI)
	default:
		ctx.Log.Error("vendor is not supported yet")
		errors.BadParameter("vendor").Http(w)
		return
	}

	// ************************ Update token ************************ //

	vendorInfo := client.GetVendorInfo()

	oaModel, err := ctx.Storage.Vendor().Get(session.Username, vendorInfo.Vendor)
	if err != nil {
		ctx.Log.Error(err)
		errors.HTTP.InternalServerError(w)
	}

	token, modify, err := client.RefreshToken(oaModel.Token)
	if err != nil {
		ctx.Log.Error(err)
		errors.HTTP.InternalServerError(w)
	}

	u, err := client.GetUser(token)
	if err != nil {
		ctx.Log.Error(err)
		errors.HTTP.InternalServerError(w)
	}

	if modify {
		oaModel.Host = vendorInfo.Host
		oaModel.Vendor = vendorInfo.Vendor
		oaModel.ServiceID = u.ServiceID
		oaModel.Token = token
		oaModel.Username = u.Username

		if err = ctx.Storage.Vendor().Update(session.Username, oaModel); err != nil {
			ctx.Log.Error(err)
			errors.HTTP.InternalServerError(w)
		}
	}

	// ************************ End update token ************************ //

	branches, err := client.ListBranches(token, u.Username, repo)
	if err != nil {
		ctx.Log.Error(err)
		errors.HTTP.InternalServerError(w)
		return
	}

	br := types.VCSBranchList{}
	br.Convert(branches)
	response, err := br.ToJson()

	w.WriteHeader(200)
	w.Write(response)
}
