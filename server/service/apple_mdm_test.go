package service

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fleetdm/fleet/v4/server/authz"
	"github.com/fleetdm/fleet/v4/server/config"
	"github.com/fleetdm/fleet/v4/server/contexts/viewer"
	"github.com/fleetdm/fleet/v4/server/datastore/mysql"
	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/fleetdm/fleet/v4/server/mock"
	"github.com/fleetdm/fleet/v4/server/ptr"
	"github.com/fleetdm/fleet/v4/server/test"
	"github.com/google/uuid"
	nanodep_client "github.com/micromdm/nanodep/client"
	nanodep_storage "github.com/micromdm/nanodep/storage"
	"github.com/micromdm/nanomdm/mdm"
	nanomdm_push "github.com/micromdm/nanomdm/push"
	"github.com/micromdm/nanomdm/storage"
	"github.com/stretchr/testify/require"
)

type dummyDEPStorage struct {
	nanodep_storage.AllStorage
	testAuthAddr string
}

func (d dummyDEPStorage) RetrieveAuthTokens(ctx context.Context, name string) (*nanodep_client.OAuth1Tokens, error) {
	return &nanodep_client.OAuth1Tokens{}, nil
}

func (d dummyDEPStorage) RetrieveConfig(context.Context, string) (*nanodep_client.Config, error) {
	return &nanodep_client.Config{
		BaseURL: d.testAuthAddr,
	}, nil
}

type dummyMDMStorage struct {
	*mysql.NanoMDMStorage
}

func (d dummyMDMStorage) EnqueueCommand(ctx context.Context, id []string, cmd *mdm.Command) (map[string]error, error) {
	return nil, nil
}

type dummyMDMPusher struct{}

func (d dummyMDMPusher) Push(context.Context, []string) (map[string]*nanomdm_push.Response, error) {
	return nil, nil
}

func setupAppleMDMService(t *testing.T, mdmStorage storage.AllStorage, depStorage nanodep_storage.AllStorage, mdmPusher nanomdm_push.Pusher) (fleet.Service, context.Context, *mock.Store) {
	ds := new(mock.Store)
	cfg := config.TestConfig()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/server/devices"):
			_, err := w.Write([]byte("{}"))
			require.NoError(t, err)
			return
		case strings.Contains(r.URL.Path, "/session"):
			_, err := w.Write([]byte(`{"auth_session_token": "yoo"}`))
			require.NoError(t, err)
			return
		}
	}))

	opts := &TestServerOpts{
		FleetConfig: &cfg,
		MDMStorage:  dummyMDMStorage{},
		DEPStorage:  dummyDEPStorage{testAuthAddr: ts.URL},
		MDMPusher:   dummyMDMPusher{},
	}
	if mdmStorage != nil {
		opts.MDMStorage = mdmStorage
	}
	if depStorage != nil {
		opts.DEPStorage = depStorage
	}
	if mdmPusher != nil {
		opts.MDMPusher = mdmPusher
	}
	svc, ctx := newTestServiceWithConfig(t, ds, cfg, nil, nil, opts)

	ds.AppConfigFunc = func(ctx context.Context) (*fleet.AppConfig, error) {
		return &fleet.AppConfig{
			OrgInfo: fleet.OrgInfo{
				OrgName: "Foo Inc.",
			},
			ServerSettings: fleet.ServerSettings{
				ServerURL: "https://foo.example.com",
			},
		}, nil
	}
	ds.GetMDMAppleEnrollmentProfileByTokenFunc = func(ctx context.Context, token string) (*fleet.MDMAppleEnrollmentProfile, error) {
		return nil, nil
	}
	ds.NewMDMAppleEnrollmentProfileFunc = func(ctx context.Context, enrollmentPayload fleet.MDMAppleEnrollmentProfilePayload) (*fleet.MDMAppleEnrollmentProfile, error) {
		return &fleet.MDMAppleEnrollmentProfile{
			ID:            1,
			Token:         "foo",
			Type:          fleet.MDMAppleEnrollmentTypeManual,
			EnrollmentURL: "https://foo.example.com?token=foo",
		}, nil
	}
	ds.GetMDMAppleEnrollmentProfileByTokenFunc = func(ctx context.Context, token string) (*fleet.MDMAppleEnrollmentProfile, error) {
		return nil, nil
	}
	ds.ListMDMAppleEnrollmentProfilesFunc = func(ctx context.Context) ([]*fleet.MDMAppleEnrollmentProfile, error) {
		return nil, nil
	}
	ds.GetMDMAppleCommandResultsFunc = func(ctx context.Context, commandUUID string) (map[string]*fleet.MDMAppleCommandResult, error) {
		return nil, nil
	}
	ds.NewMDMAppleInstallerFunc = func(ctx context.Context, name string, size int64, manifest string, installer []byte, urlToken string) (*fleet.MDMAppleInstaller, error) {
		return nil, nil
	}
	ds.MDMAppleInstallerFunc = func(ctx context.Context, token string) (*fleet.MDMAppleInstaller, error) {
		return nil, nil
	}
	ds.MDMAppleInstallerDetailsByIDFunc = func(ctx context.Context, id uint) (*fleet.MDMAppleInstaller, error) {
		return nil, nil
	}
	ds.DeleteMDMAppleInstallerFunc = func(ctx context.Context, id uint) error {
		return nil
	}
	ds.MDMAppleInstallerDetailsByTokenFunc = func(ctx context.Context, token string) (*fleet.MDMAppleInstaller, error) {
		return nil, nil
	}
	ds.ListMDMAppleInstallersFunc = func(ctx context.Context) ([]fleet.MDMAppleInstaller, error) {
		return nil, nil
	}
	ds.MDMAppleListDevicesFunc = func(ctx context.Context) ([]fleet.MDMAppleDevice, error) {
		return nil, nil
	}
	ds.GetNanoMDMEnrollmentStatusFunc = func(ctx context.Context, hostUUID string) (bool, error) {
		return false, nil
	}

	return svc, ctx, ds
}

func TestAppleMDMAuthorization(t *testing.T) {
	svc, ctx, _ := setupAppleMDMService(t, nil, nil, nil)

	checkAuthErr := func(t *testing.T, err error, shouldFailWithAuth bool) {
		t.Helper()

		if shouldFailWithAuth {
			require.Error(t, err)
			require.Contains(t, err.Error(), authz.ForbiddenErrorMessage)
		} else {
			require.NoError(t, err)
		}
	}

	testAuthdMethods := func(t *testing.T, user *fleet.User, shouldFailWithAuth bool) {
		ctx := test.UserContext(ctx, user)
		_, err := svc.NewMDMAppleEnrollmentProfile(ctx, fleet.MDMAppleEnrollmentProfilePayload{})
		checkAuthErr(t, err, shouldFailWithAuth)
		_, err = svc.ListMDMAppleEnrollmentProfiles(ctx)
		checkAuthErr(t, err, shouldFailWithAuth)
		_, err = svc.GetMDMAppleCommandResults(ctx, "foo")
		checkAuthErr(t, err, shouldFailWithAuth)
		_, err = svc.UploadMDMAppleInstaller(ctx, "foo", 3, bytes.NewReader([]byte("foo")))
		checkAuthErr(t, err, shouldFailWithAuth)
		_, err = svc.GetMDMAppleInstallerByID(ctx, 42)
		checkAuthErr(t, err, shouldFailWithAuth)
		err = svc.DeleteMDMAppleInstaller(ctx, 42)
		checkAuthErr(t, err, shouldFailWithAuth)
		_, err = svc.ListMDMAppleInstallers(ctx)
		checkAuthErr(t, err, shouldFailWithAuth)
		_, err = svc.ListMDMAppleDevices(ctx)
		checkAuthErr(t, err, shouldFailWithAuth)
		_, err = svc.ListMDMAppleDEPDevices(ctx)
		checkAuthErr(t, err, shouldFailWithAuth)
		_, _, err = svc.EnqueueMDMAppleCommand(ctx, &fleet.MDMAppleCommand{Command: &mdm.Command{}}, nil, false)
		checkAuthErr(t, err, shouldFailWithAuth)
	}

	// Only global admins can access the endpoints.
	testAuthdMethods(t, test.UserAdmin, false)

	// All other users should not have access to the endpoints.
	for _, user := range []*fleet.User{
		test.UserNoRoles,
		test.UserMaintainer,
		test.UserObserver,
		test.UserTeamAdminTeam1,
	} {
		testAuthdMethods(t, user, true)
	}
	// Token authenticated endpoints can be accessed by anyone.
	ctx = test.UserContext(ctx, test.UserNoRoles)
	_, err := svc.GetMDMAppleInstallerByToken(ctx, "foo")
	require.NoError(t, err)
	_, err = svc.GetMDMAppleEnrollmentProfileByToken(ctx, "foo")
	require.NoError(t, err)
	_, err = svc.GetMDMAppleInstallerDetailsByToken(ctx, "foo")
	require.NoError(t, err)
	// Generating a new key pair does not actually make any changes to fleet, or expose any
	// information. The user must configure fleet with the new key pair and restart the server.
	_, err = svc.NewMDMAppleDEPKeyPair(ctx)
	require.NoError(t, err)

	// Must be device-authenticated, should fail
	_, err = svc.GetDeviceMDMAppleEnrollmentProfile(ctx)
	checkAuthErr(t, err, true)
	// works with device-authenticated context
	ctx = test.HostContext(context.Background(), &fleet.Host{})
	_, err = svc.GetDeviceMDMAppleEnrollmentProfile(ctx)
	require.NoError(t, err)
}

func TestMDMAppleEnrollURL(t *testing.T) {
	svc := Service{}

	cases := []struct {
		appConfig   *fleet.AppConfig
		expectedURL string
	}{
		{
			appConfig: &fleet.AppConfig{
				ServerSettings: fleet.ServerSettings{
					ServerURL: "https://foo.example.com",
				},
			},
			expectedURL: "https://foo.example.com/api/mdm/apple/enroll?token=tok",
		},
		{
			appConfig: &fleet.AppConfig{
				ServerSettings: fleet.ServerSettings{
					ServerURL: "https://foo.example.com/",
				},
			},
			expectedURL: "https://foo.example.com/api/mdm/apple/enroll?token=tok",
		},
	}

	for _, tt := range cases {
		enrollURL, err := svc.mdmAppleEnrollURL("tok", tt.appConfig)
		require.NoError(t, err)
		require.Equal(t, tt.expectedURL, enrollURL)
	}
}

func TestAppleMDMEnrollmentProfile(t *testing.T) {
	svc, ctx, _ := setupAppleMDMService(t, nil, nil, nil)

	// Only global admins can create enrollment profiles.
	ctx = test.UserContext(ctx, test.UserAdmin)
	_, err := svc.NewMDMAppleEnrollmentProfile(ctx, fleet.MDMAppleEnrollmentProfilePayload{})
	require.NoError(t, err)

	// All other users should not have access to the endpoints.
	for _, user := range []*fleet.User{
		test.UserNoRoles,
		test.UserMaintainer,
		test.UserObserver,
		test.UserTeamAdminTeam1,
	} {
		ctx := test.UserContext(ctx, user)
		_, err := svc.NewMDMAppleEnrollmentProfile(ctx, fleet.MDMAppleEnrollmentProfilePayload{})
		require.Error(t, err)
		require.Contains(t, err.Error(), authz.ForbiddenErrorMessage)
	}
}

type noErrorPusher struct{}

// Push simulates successful push responses. The result maps each of the provided deviceIDs to a
// internally generated UUID, which is intended here to mock the APNs API response.
func (nep *noErrorPusher) Push(ctx context.Context, deviceIDs []string) (map[string]*nanomdm_push.Response, error) {
	res := make(map[string]*nanomdm_push.Response)
	for _, s := range deviceIDs {
		res[s] = &nanomdm_push.Response{Id: uuid.New().String()}
	}
	return res, nil
}

func TestMDMCommandAuthz(t *testing.T) {
	pusher := noErrorPusher{}

	svc, ctx, ds := setupAppleMDMService(t, nil, nil, &pusher)

	ds.HostLiteFunc = func(ctx context.Context, hostID uint) (*fleet.Host, error) {
		switch hostID {
		case 1:
			return &fleet.Host{UUID: "test-host-team-1", TeamID: ptr.Uint(1)}, nil
		default:
			return &fleet.Host{UUID: "test-host-no-team"}, nil
		}
	}

	ds.GetHostMDMCheckinInfoFunc = func(ctx context.Context, hostUUID string) (*fleet.HostMDMCheckinInfo, error) {
		return &fleet.HostMDMCheckinInfo{}, nil
	}

	ds.NewActivityFunc = func(context.Context, *fleet.User, fleet.ActivityDetails) error {
		return nil
	}

	var mdmEnabled atomic.Bool
	ds.GetNanoMDMEnrollmentStatusFunc = func(ctx context.Context, hostUUID string) (bool, error) {
		// This function is called twice during EnqueueMDMAppleCommandRemoveEnrollmentProfile.
		// It first is called to check that the device is enrolled as a pre-condition to enqueueing the
		// command. It is called second time after the command has been enqueued to check whether
		// the device was successfully unenrolled.
		//
		// For each test run, the bool should be initialized to true to simulate an existing device
		// that is initially enrolled to Fleet's MDM.
		return mdmEnabled.Swap(!mdmEnabled.Load()), nil
	}

	testCases := []struct {
		name             string
		user             *fleet.User
		shouldFailGlobal bool
		shouldFailTeam   bool
	}{
		{
			"global admin",
			&fleet.User{GlobalRole: ptr.String(fleet.RoleAdmin)},
			false,
			false,
		},
		{
			"global maintainer",
			&fleet.User{GlobalRole: ptr.String(fleet.RoleMaintainer)},
			false,
			false,
		},
		{
			"global observer",
			&fleet.User{GlobalRole: ptr.String(fleet.RoleObserver)},
			true,
			true,
		},
		{
			"team admin, belongs to team",
			&fleet.User{Teams: []fleet.UserTeam{{Team: fleet.Team{ID: 1}, Role: fleet.RoleAdmin}}},
			true,
			false,
		},
		{
			"team admin, DOES NOT belong to team",
			&fleet.User{Teams: []fleet.UserTeam{{Team: fleet.Team{ID: 2}, Role: fleet.RoleAdmin}}},
			true,
			true,
		},
		{
			"team maintainer, belongs to team",
			&fleet.User{Teams: []fleet.UserTeam{{Team: fleet.Team{ID: 1}, Role: fleet.RoleMaintainer}}},
			true,
			false,
		},
		{
			"team maintainer, DOES NOT belong to team",
			&fleet.User{Teams: []fleet.UserTeam{{Team: fleet.Team{ID: 2}, Role: fleet.RoleMaintainer}}},
			true,
			true,
		},
		{
			"team observer, belongs to team",
			&fleet.User{Teams: []fleet.UserTeam{{Team: fleet.Team{ID: 1}, Role: fleet.RoleObserver}}},
			true,
			true,
		},
		{
			"team observer, DOES NOT belong to team",
			&fleet.User{Teams: []fleet.UserTeam{{Team: fleet.Team{ID: 2}, Role: fleet.RoleObserver}}},
			true,
			true,
		},
		{
			"user no roles",
			&fleet.User{ID: 1337},
			true,
			true,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			ctx := viewer.NewContext(ctx, viewer.Viewer{User: tt.user})

			mdmEnabled.Store(true)
			err := svc.EnqueueMDMAppleCommandRemoveEnrollmentProfile(ctx, 42) // global host
			if !tt.shouldFailGlobal {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), authz.ForbiddenErrorMessage)
			}

			mdmEnabled.Store(true)
			err = svc.EnqueueMDMAppleCommandRemoveEnrollmentProfile(ctx, 1) // host belongs to team 1
			if !tt.shouldFailTeam {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), authz.ForbiddenErrorMessage)
			}
		})
	}
}

func TestMDMAuthenticate(t *testing.T) {
	ds := new(mock.Store)
	svc := MDMAppleCheckinAndCommandService{ds: ds}
	ctx := context.Background()
	uuid, serial, model := "ABC-DEF-GHI", "XYZABC", "MacBookPro 16,1"

	ds.IngestMDMAppleDeviceFromCheckinFunc = func(ctx context.Context, mdmHost fleet.MDMAppleHostDetails) error {
		require.Equal(t, uuid, mdmHost.UDID)
		require.Equal(t, serial, mdmHost.SerialNumber)
		require.Equal(t, model, mdmHost.Model)
		return nil
	}

	ds.GetHostMDMCheckinInfoFunc = func(ct context.Context, hostUUID string) (*fleet.HostMDMCheckinInfo, error) {
		require.Equal(t, uuid, hostUUID)
		return &fleet.HostMDMCheckinInfo{HardwareSerial: serial, DisplayName: fmt.Sprintf("%s (%s)", model, serial), InstalledFromDEP: false}, nil
	}

	ds.NewActivityFunc = func(ctx context.Context, user *fleet.User, activity fleet.ActivityDetails) error {
		a, ok := activity.(*fleet.ActivityTypeMDMEnrolled)
		require.True(t, ok)
		require.Nil(t, user)
		require.Equal(t, "mdm_enrolled", activity.ActivityName())
		require.Equal(t, serial, a.HostSerial)
		require.Equal(t, a.HostDisplayName, fmt.Sprintf("%s (%s)", model, serial))
		require.False(t, a.InstalledFromDEP)
		return nil
	}

	err := svc.Authenticate(
		&mdm.Request{Context: ctx},
		&mdm.Authenticate{
			Enrollment: mdm.Enrollment{
				UDID: uuid,
			},
			SerialNumber: serial,
			Model:        model,
		},
	)
	require.NoError(t, err)
	require.True(t, ds.IngestMDMAppleDeviceFromCheckinFuncInvoked)
	require.True(t, ds.GetHostMDMCheckinInfoFuncInvoked)
	require.True(t, ds.NewActivityFuncInvoked)
}

func TestMDMCheckout(t *testing.T) {
	ds := new(mock.Store)
	svc := MDMAppleCheckinAndCommandService{ds: ds}
	ctx := context.Background()
	uuid, serial, installedFromDEP, displayName := "ABC-DEF-GHI", "XYZABC", true, "Test's MacBook"

	ds.UpdateHostTablesOnMDMUnenrollFunc = func(ctx context.Context, hostUUID string) error {
		require.Equal(t, uuid, hostUUID)
		return nil
	}

	ds.GetHostMDMCheckinInfoFunc = func(ct context.Context, hostUUID string) (*fleet.HostMDMCheckinInfo, error) {
		require.Equal(t, uuid, hostUUID)
		return &fleet.HostMDMCheckinInfo{
			HardwareSerial:   serial,
			DisplayName:      displayName,
			InstalledFromDEP: installedFromDEP,
		}, nil
	}

	ds.NewActivityFunc = func(ctx context.Context, user *fleet.User, activity fleet.ActivityDetails) error {
		a, ok := activity.(*fleet.ActivityTypeMDMUnenrolled)
		require.True(t, ok)
		require.Nil(t, user)
		require.Equal(t, "mdm_unenrolled", activity.ActivityName())
		require.Equal(t, serial, a.HostSerial)
		require.Equal(t, displayName, a.HostDisplayName)
		require.True(t, a.InstalledFromDEP)
		return nil
	}

	err := svc.CheckOut(
		&mdm.Request{Context: ctx},
		&mdm.CheckOut{
			Enrollment: mdm.Enrollment{
				UDID: uuid,
			},
		},
	)
	require.NoError(t, err)
	require.True(t, ds.UpdateHostTablesOnMDMUnenrollFuncInvoked)
	require.True(t, ds.GetHostMDMCheckinInfoFuncInvoked)
	require.True(t, ds.NewActivityFuncInvoked)
}
