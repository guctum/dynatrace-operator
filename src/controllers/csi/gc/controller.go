package csigc

import (
	"context"
	"sync"
	"time"

	dynatracev1beta1 "github.com/Dynatrace/dynatrace-operator/src/api/v1beta1"
	dtcsi "github.com/Dynatrace/dynatrace-operator/src/controllers/csi"
	"github.com/Dynatrace/dynatrace-operator/src/controllers/csi/metadata"
	"github.com/pkg/errors"
	"github.com/spf13/afero"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const gcInterval = 5 * time.Minute

// can contain the tag of the image or the digest, depending on how the user provided the image
// or the version set for the download
type pinnedVersionSet map[string]bool

func (set pinnedVersionSet) isNotPinned(version string) bool {
	return !set[version]
}

// tenantInfo stores tenant specific information
// used to delete unused files or directories connected to that tenant
type tenantInfo struct {
	uuid               string
	pinnedVersions     pinnedVersionSet
}

func (info tenantInfo) addPinnedVersion(version string) {
	if version == "" {
		return
	}
	if info.pinnedVersions == nil {
		info.pinnedVersions = make(pinnedVersionSet)
	}
	info.pinnedVersions[version] = true
}

type garbageCollectionState struct {
	tenantInfos     map[string]tenantInfo
	staleMetadata   map[string]metadata.Dynakube
	currentMetadata map[string]metadata.Dynakube
}

// CSIGarbageCollector removes unused and outdated agent versions
type CSIGarbageCollector struct {
	apiReader client.Reader
	namespace string
	fs        afero.Fs
	db        metadata.Access
	path      metadata.PathResolver
	timer     *time.Timer
	safeToRun *sync.Mutex
}

// NewCSIGarbageCollector returns a new CSIGarbageCollector
func NewCSIGarbageCollector(apiReader client.Reader, opts dtcsi.CSIOptions, db metadata.Access, namespace string) *CSIGarbageCollector {
	return &CSIGarbageCollector{
		apiReader: apiReader,
		namespace: namespace,
		fs:        afero.NewOsFs(),
		db:        db,
		path:      metadata.PathResolver{RootDir: opts.RootDir},
		timer:     time.NewTimer(gcInterval),
		safeToRun: &sync.Mutex{},
	}
}

func (gc *CSIGarbageCollector) Start(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-gc.timer.C:
			gc.SetUnsafeToRun()
			err := gc.cleanUp(ctx)
			if err != nil {
				return err
			}
			gc.SetSafeToRun()
		}
	}
}

func (gc *CSIGarbageCollector) SetUnsafeToRun() { // TODO confusing name
	gc.safeToRun.Lock()
	gc.timer.Stop()
}

func (gc *CSIGarbageCollector) SetSafeToRun() { // TODO confusing name
	gc.timer.Stop()
	gc.timer.Reset(gcInterval)
	gc.safeToRun.Unlock()
}

func (gc *CSIGarbageCollector) cleanUp(ctx context.Context) error {
	dynakubeList, err := getAllDynakubes(ctx, gc.apiReader, gc.namespace)
	if err != nil {
		return err
	}

	dynakubeMetadata, err := gc.db.GetAllDynakubes()
	if err != nil {
		return err
	}

	state := gc.determineState(dynakubeList.Items, dynakubeMetadata)

	for _, tenant := range state.tenantInfos {
		log.Info("Running binary garbage collection for tenant", "tenantUUID", tenant.uuid)
		gc.runBinaryGarbageCollection(tenant.pinnedVersions, tenant.uuid)
		log.Info("Running log garbage collection for tenant", "tenantUUID", tenant.uuid)
		gc.runLogGarbageCollection(tenant.uuid)
	}

	log.Info("running shared images garbage collection")
	if err := gc.runSharedImagesGarbageCollection(); err != nil {
		log.Info("failed to garbage collect the shared images")
		return err
	}

	return nil
}

func (gc *CSIGarbageCollector) determineState(dynakubes []dynatracev1beta1.DynaKube, dynakubeMetadata []*metadata.Dynakube) garbageCollectionState {
	currentMetadata := make(map[string]metadata.Dynakube)
	staleMetadata := make(map[string]metadata.Dynakube)
	tenantInfos := make(map[string]tenantInfo)
	for _, meta := range dynakubeMetadata {
		staleMetadata[meta.Name] = *meta
		currentMetadata[meta.Name] = *meta
	}
	for _, dynakube := range dynakubes {
		if !dynakube.NeedsCSIDriver() {
			continue
		}
		currentMetadata[dynakube.Name] = staleMetadata[dynakube.Name]
		delete(staleMetadata, dynakube.Name)
		tenantUUID := currentMetadata[dynakube.Name].TenantUUID
		latestVersion := currentMetadata[dynakube.Name].LatestVersion
		info, ok := tenantInfos[tenantUUID]
		if !ok {
			info = tenantInfo{
				uuid:               tenantUUID,
				pinnedVersions:     make(pinnedVersionSet),
			}
			tenantInfos[tenantUUID] = info
		}
		info.addPinnedVersion(dynakube.CodeModulesVersion())
		info.addPinnedVersion(latestVersion)
	}
	return garbageCollectionState{
		staleMetadata:   staleMetadata,
		currentMetadata: currentMetadata,
		tenantInfos:     tenantInfos,
	}
}

func getAllDynakubes(ctx context.Context, apiReader client.Reader, namespace string) (*dynatracev1beta1.DynaKubeList, error) {
	var dynakubeList dynatracev1beta1.DynaKubeList
	if err := apiReader.List(ctx, &dynakubeList, client.InNamespace(namespace)); err != nil {
		log.Info("failed to get all DynaKube objects")
		return nil, errors.WithStack(err)
	}
	return &dynakubeList, nil
}
