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

func getLatestTemplateVersion(ctx context.Context, serviceHandler *service.ServiceHandler, fleetName string) (*api.TemplateVersion, error) {
	response, err := serviceHandler.ReadLatestTemplateVersion(ctx, server.ReadTemplateVersionRequestObject{Fleet: fleetName})
	if err != nil {
		return nil, fmt.Errorf("failed getting latest templateVersion: %w", err)
	}

	var templateVersion api.TemplateVersion
	switch resp := response.(type) {
	case server.ReadTemplateVersion200JSONResponse:
		templateVersion = api.TemplateVersion(resp)
	default:
		return nil, fmt.Errorf("failed getting latest templateVersion: %s", server.PrintResponse(resp))
	}
	return &templateVersion, nil
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
