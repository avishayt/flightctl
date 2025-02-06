package tasks

import (
	"context"
	"errors"
	"fmt"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/api/server"
	"github.com/flightctl/flightctl/internal/service"
	"github.com/flightctl/flightctl/internal/util"
)

const ItemsPerPage = 1000

var (
	ErrUnknownConfigName      = errors.New("failed to find configuration item name")
	ErrUnknownApplicationType = errors.New("unknown application type")
)

func getOwnerFleet(device *api.Device) (string, bool, error) {
	if device.Metadata.Owner == nil {
		return "", true, nil
	}

	ownerType, ownerName, err := util.GetResourceOwner(device.Metadata.Owner)
	if err != nil {
		return "", false, err
	}

	if ownerType != api.FleetKind {
		return "", false, nil
	}

	return ownerName, true, nil
}

func getDevice(ctx context.Context, serviceHandler *service.ServiceHandler, name string) (*api.Device, error) {
	response, err := serviceHandler.ReadDevice(ctx, server.ReadDeviceRequestObject{Name: name})
	if err != nil {
		return nil, fmt.Errorf("failed getting device: %w", err)
	}
	var device api.Device
	switch resp := response.(type) {
	case server.ReadDevice200JSONResponse:
		device = api.Device(resp)
	default:
		return nil, fmt.Errorf("failed getting device: %s", server.PrintResponse(resp))
	}
	return &device, nil
}

func patchDevice(ctx context.Context, serviceHandler *service.ServiceHandler, name string, patch api.PatchRequest) (*api.Device, bool, error) {
	response, err := serviceHandler.PatchDevice(ctx, server.PatchDeviceRequestObject{Name: name, Body: &patch})
	if err != nil {
		return nil, false, fmt.Errorf("failed patching device: %w", err)
	}
	var device api.Device
	switch resp := response.(type) {
	case server.PatchDevice200JSONResponse:
		device = api.Device(resp)
	case server.PatchDevice409JSONResponse:
		return nil, true, fmt.Errorf("failed patching device: %s", server.PrintResponse(resp))
	default:
		return nil, false, fmt.Errorf("failed patching device: %s", server.PrintResponse(resp))
	}
	return &device, false, nil
}

func listDevices(ctx context.Context, serviceHandler *service.ServiceHandler, params api.ListDevicesParams) (*api.DeviceList, error) {
	response, err := serviceHandler.ListDevices(ctx, server.ListDevicesRequestObject{Params: params})
	if err != nil {
		return nil, fmt.Errorf("failed fetching devices: %w", err)
	}
	var deviceList api.DeviceList
	switch resp := response.(type) {
	case server.ListDevices200JSONResponse:
		deviceList = api.DeviceList(resp)
	default:
		return nil, fmt.Errorf("failed fetching devices: %s", server.PrintResponse(resp))
	}
	return &deviceList, nil
}

func getRepository(ctx context.Context, serviceHandler *service.ServiceHandler, name string) (*api.Repository, error) {
	response, err := serviceHandler.ReadRepository(ctx, server.ReadRepositoryRequestObject{Name: name})
	if err != nil {
		return nil, fmt.Errorf("failed getting repository: %w", err)
	}
	var repository api.Repository
	switch resp := response.(type) {
	case server.ReadRepository200JSONResponse:
		repository = api.Repository(resp)
	default:
		return nil, fmt.Errorf("failed getting repository: %s", server.PrintResponse(resp))
	}
	return &repository, nil
}

func listRepositories(ctx context.Context, serviceHandler *service.ServiceHandler, params api.ListRepositoriesParams) (*api.RepositoryList, error) {
	response, err := serviceHandler.ListRepositories(ctx, server.ListRepositoriesRequestObject{Params: params})
	if err != nil {
		return nil, fmt.Errorf("failed fetching repositories: %w", err)
	}
	var repoList api.RepositoryList
	switch resp := response.(type) {
	case server.ListRepositories200JSONResponse:
		repoList = api.RepositoryList(resp)
	default:
		return nil, fmt.Errorf("failed fetching repositories: %s", server.PrintResponse(resp))
	}
	return &repoList, nil
}

func listResourceSyncs(ctx context.Context, serviceHandler *service.ServiceHandler, params api.ListResourceSyncsParams) (*api.ResourceSyncList, error) {
	response, err := serviceHandler.ListResourceSyncs(ctx, server.ListResourceSyncsRequestObject{Params: params})
	if err != nil {
		return nil, fmt.Errorf("failed fetching resourceSyncs: %w", err)
	}
	var rsList api.ResourceSyncList
	switch resp := response.(type) {
	case server.ListResourceSyncs200JSONResponse:
		rsList = api.ResourceSyncList(resp)
	default:
		return nil, fmt.Errorf("failed fetching resourceSyncs: %s", server.PrintResponse(resp))
	}
	return &rsList, nil
}

func getFleet(ctx context.Context, serviceHandler *service.ServiceHandler, name string) (*api.Fleet, error) {
	response, err := serviceHandler.ReadFleet(ctx, server.ReadFleetRequestObject{Name: name})
	if err != nil {
		return nil, fmt.Errorf("failed getting fleet: %w", err)
	}
	var fleet api.Fleet
	switch resp := response.(type) {
	case server.ReadFleet200JSONResponse:
		fleet = api.Fleet(resp)
	default:
		return nil, fmt.Errorf("failed getting fleet: %s", server.PrintResponse(resp))
	}
	return &fleet, nil
}

func listFleets(ctx context.Context, serviceHandler *service.ServiceHandler, params api.ListFleetsParams) (*api.FleetList, error) {
	response, err := serviceHandler.ListFleets(ctx, server.ListFleetsRequestObject{Params: params})
	if err != nil {
		return nil, fmt.Errorf("failed fetching fleets: %w", err)
	}
	var fleetList api.FleetList
	switch resp := response.(type) {
	case server.ListFleets200JSONResponse:
		fleetList = api.FleetList(resp)
	default:
		return nil, fmt.Errorf("failed fetching fleets: %s", server.PrintResponse(resp))
	}
	return &fleetList, nil
}

func deleteFleet(ctx context.Context, serviceHandler *service.ServiceHandler, name string) (*api.Fleet, error) {
	response, err := serviceHandler.DeleteFleet(ctx, server.DeleteFleetRequestObject{Name: name})
	if err != nil {
		return nil, fmt.Errorf("failed deleting fleet: %w", err)
	}
	var fleet api.Fleet
	switch resp := response.(type) {
	case server.DeleteFleet200JSONResponse:
		fleet = api.Fleet(resp)
	default:
		return nil, fmt.Errorf("failed deleting fleet: %s", server.PrintResponse(resp))
	}
	return &fleet, nil
}

func replaceFleet(ctx context.Context, serviceHandler *service.ServiceHandler, f *api.Fleet) (*api.Fleet, error) {
	response, err := serviceHandler.ReplaceFleet(ctx, server.ReplaceFleetRequestObject{Name: *f.Metadata.Name, Body: f})
	if err != nil {
		return nil, fmt.Errorf("failed replacing fleet: %w", err)
	}
	var fleet api.Fleet
	switch resp := response.(type) {
	case server.ReplaceFleet200JSONResponse:
		fleet = api.Fleet(resp)
	default:
		return nil, fmt.Errorf("failed replacing fleet: %s", server.PrintResponse(resp))
	}
	return &fleet, nil
}

func replaceFleetStatus(ctx context.Context, serviceHandler *service.ServiceHandler, f *api.Fleet) (*api.Fleet, error) {
	response, err := serviceHandler.ReplaceFleetStatus(ctx, server.ReplaceFleetStatusRequestObject{Name: *f.Metadata.Name, Body: f})
	if err != nil {
		return nil, fmt.Errorf("failed replacing fleet status: %w", err)
	}
	var fleet api.Fleet
	switch resp := response.(type) {
	case server.ReplaceFleetStatus200JSONResponse:
		fleet = api.Fleet(resp)
	default:
		return nil, fmt.Errorf("failed replacing fleet status: %s", server.PrintResponse(resp))
	}
	return &fleet, nil
}
