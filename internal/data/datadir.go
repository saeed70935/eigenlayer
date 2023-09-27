package data

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/NethermindEth/eigenlayer/internal/locker"
	"github.com/NethermindEth/eigenlayer/internal/package_handler"
	"github.com/NethermindEth/eigenlayer/internal/utils"
	"github.com/spf13/afero"
)

const (
	nodesDirName = "nodes"
	tempDir      = "temp"
	pluginsDir   = "plugin"
	backupDir    = "backup"
)

const monitoringStackDirName = "monitoring"

// DataDir is the directory where all the data is stored.
type DataDir struct {
	path   string
	fs     afero.Fs
	locker locker.Locker
}

// NewDataDir creates a new DataDir instance with the given path as root.
func NewDataDir(path string, fs afero.Fs, locker locker.Locker) (*DataDir, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	return &DataDir{path: absPath, fs: fs, locker: locker}, nil
}

// Path returns the path of the data dir.
func (d *DataDir) Path() string {
	return d.path
}

// NewDataDirDefault creates a new DataDir instance with the default path as root.
// Default path is $XDG_DATA_HOME/.eigen or $HOME/.local/share/.eigen if $XDG_DATA_HOME is not set
// as defined in the XDG Base Directory Specification
func NewDataDirDefault(fs afero.Fs, locker locker.Locker) (*DataDir, error) {
	userDataHome := os.Getenv("XDG_DATA_HOME")
	if userDataHome == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		userDataHome = filepath.Join(userHome, ".local", "share")
	}
	dataDir := filepath.Join(userDataHome, ".eigen")
	err := fs.MkdirAll(dataDir, 0o755)
	if err != nil {
		return nil, err
	}

	return NewDataDir(dataDir, fs, locker)
}

// Instance returns the instance with the given id.
func (d *DataDir) Instance(instanceId string) (*Instance, error) {
	instancePath := filepath.Join(d.path, nodesDirName, instanceId)
	return newInstance(instancePath, d.fs, d.locker)
}

type AddInstanceOptions struct {
	URL            string
	Version        string
	Profile        string
	Tag            string
	PackageHandler *package_handler.PackageHandler
	Env            map[string]string
}

// InitInstance initializes a new instance. If an instance with the same id already
// exists, an error is returned.
func (d *DataDir) InitInstance(instance *Instance) error {
	instancePath := filepath.Join(d.path, nodesDirName, InstanceId(instance.Name, instance.Tag))
	_, err := d.fs.Stat(instancePath)
	if err != nil && os.IsNotExist(err) {
		return instance.init(instancePath, d.fs, d.locker)
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: %s", ErrInstanceAlreadyExists, InstanceId(instance.Name, instance.Tag))
}

// HasInstance returns true if an instance with the given id already exists in the
// data dir.
func (d *DataDir) HasInstance(instanceId string) bool {
	instancePath := filepath.Join(d.path, nodesDirName, instanceId)
	_, err := d.fs.Stat(instancePath)
	return err == nil
}

// InstancePath return the path to the directory of the instance with the given id.
func (d *DataDir) InstancePath(instanceId string) (string, error) {
	instancePath := filepath.Join(d.path, nodesDirName, instanceId)
	_, err := d.fs.Stat(instancePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrInstanceNotFound
		}
		return "", err
	}
	return instancePath, nil
}

// RemoveInstance removes the instance with the given id.
func (d *DataDir) RemoveInstance(instanceId string) error {
	instancePath := filepath.Join(d.path, nodesDirName, instanceId)
	instanceDir, err := d.fs.Stat(instancePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrInstanceNotFound, instanceId)
		}
		return err
	}
	if !instanceDir.IsDir() {
		return fmt.Errorf("%s is not a directory", instanceId)
	}
	return d.fs.RemoveAll(instancePath)
}

// InitTemp creates a new temporary directory for the given id. If already exists,
// an error is returned.
func (d *DataDir) InitTemp(id string) (string, error) {
	tempPath := filepath.Join(d.path, tempDir, id)
	_, err := d.fs.Stat(tempPath)
	if err != nil {
		if os.IsNotExist(err) {
			return tempPath, d.fs.MkdirAll(tempPath, 0o755)
		}
		return "", err
	}
	return "", ErrTempDirAlreadyExists
}

// RemoveTemp removes the temporary directory with the given id.
func (d *DataDir) RemoveTemp(id string) error {
	return d.fs.RemoveAll(filepath.Join(d.path, tempDir, id))
}

// TempPath returns the path to the temporary directory with the given id.
func (d *DataDir) TempPath(id string) (string, error) {
	tempPath := filepath.Join(d.path, tempDir, id)
	tempStat, err := d.fs.Stat(tempPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrTempDirDoesNotExist
		}
		return "", err
	}
	if !tempStat.IsDir() {
		return "", ErrTempIsNotDir
	}
	return tempPath, nil
}

func (d *DataDir) InitBackup(backupId BackupId) (*Backup, error) {
	if err := d.initBackupDir(); err != nil {
		return nil, err
	}
	return d.initBackup(backupId)
}

func (d *DataDir) initBackup(backupId BackupId) (*Backup, error) {
	backupPath, err := d.backupPath(backupId)
	if err != nil {
		return nil, err
	}

	ok, err := d.hasBackup(backupId)
	if err != nil {
		return nil, err
	}
	if ok {
		return nil, fmt.Errorf("%w: %s", ErrBackupAlreadyExists, backupId)
	}

	err = utils.TarInit(d.fs, backupPath)
	if err != nil {
		return nil, err
	}

	return &Backup{
		Id:   backupId,
		path: backupPath,
	}, nil
}

func (d *DataDir) hasBackup(backupId BackupId) (bool, error) {
	backupPath, err := d.backupPath(backupId)
	if err != nil {
		return false, err
	}
	return afero.Exists(d.fs, backupPath)
}

func (d *DataDir) backupPath(backupId BackupId) (string, error) {
	return filepath.Join(d.path, backupDir, backupId.String()+".tar"), nil
}

func (d *DataDir) initBackupDir() error {
	backupDirPath := filepath.Join(d.path, backupDir)
	ok, err := afero.DirExists(d.fs, backupDirPath)
	if err != nil {
		return err
	}
	if !ok {
		err = d.fs.MkdirAll(backupDirPath, 0o755)
		if err != nil {
			return err
		}
	}
	return nil
}

// MonitoringStack checks if a monitoring stack directory exists in the data directory.
// If the directory does not exist, it creates it and initializes a new MonitoringStack instance.
// If the directory exists, it simply returns a new MonitoringStack instance.
// It returns an error if there is any issue accessing or creating the directory, or initializing the MonitoringStack.
func (d *DataDir) MonitoringStack() (*MonitoringStack, error) {
	monitoringStackPath := filepath.Join(d.path, monitoringStackDirName)
	_, err := d.fs.Stat(monitoringStackPath)
	if os.IsNotExist(err) {
		if err = d.fs.MkdirAll(monitoringStackPath, 0o755); err != nil {
			return nil, err
		}

		monitoringStack := &MonitoringStack{path: monitoringStackPath, fs: d.fs, l: d.locker}
		if err = monitoringStack.Init(); err != nil {
			return nil, err
		}
		return monitoringStack, nil
	} else if err != nil {
		return nil, err
	}

	return newMonitoringStack(monitoringStackPath, d.fs, d.locker), nil
}

// RemoveMonitoringStack removes the monitoring stack directory from the data directory.
// It returns an error if there is any issue accessing or removing the directory.
func (d *DataDir) RemoveMonitoringStack() error {
	monitoringStackPath := filepath.Join(d.path, monitoringStackDirName)
	_, err := d.fs.Stat(monitoringStackPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("%w: %s", ErrMonitoringStackNotFound, monitoringStackPath)
	} else if err != nil {
		return err
	}

	return d.fs.RemoveAll(monitoringStackPath)
}

// ListInstances returns the ID list of all the installed instances.
func (d *DataDir) ListInstances() ([]Instance, error) {
	nodesDirPath := filepath.Join(d.path, nodesDirName)
	_, err := d.fs.Stat(nodesDirPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty list if the nodes directory does not exist
			return []Instance{}, nil
		}
		return nil, err
	}
	dirEntries, err := afero.ReadDir(d.fs, nodesDirPath)
	if err != nil {
		return nil, err
	}
	instances := make([]Instance, 0)
	for _, dirEntry := range dirEntries {
		if dirEntry.IsDir() {
			instance, err := d.Instance(dirEntry.Name())
			if err != nil {
				return nil, err
			}
			instances = append(instances, *instance)
		}
	}
	return instances, nil
}

// SavePluginImageContext saves the plugin image context to the data dir as a tar file.
func (d *DataDir) SavePluginImageContext(id string, ctx io.ReadCloser) (err error) {
	defer ctx.Close()
	err = d.fs.MkdirAll(filepath.Join(d.path, pluginsDir), 0o755)
	if err != nil {
		return err
	}
	ctxF, err := d.fs.Create(filepath.Join(d.pluginDir(), id+".tar"))
	if err != nil {
		return err
	}
	defer func() {
		errClose := ctxF.Close()
		if err != nil {
			err = errClose
		}
	}()
	_, err = io.Copy(ctxF, ctx)
	return err
}

// GetPluginContext returns the plugin image context tar file.
func (d *DataDir) GetPluginContext(id string) (io.ReadCloser, error) {
	return d.fs.Open(filepath.Join(d.pluginDir(), id+".tar"))
}

// RemovePluginContext removes the plugin image context tar file. If the file
// does not exist, it return nil.
func (d *DataDir) RemovePluginContext(id string) error {
	fileName := filepath.Join(d.pluginDir(), id+".tar")
	exist, err := afero.Exists(d.fs, fileName)
	if err != nil {
		return err
	}
	if exist {
		return d.fs.Remove(fileName)
	}
	return nil
}

func (d *DataDir) pluginDir() string {
	return filepath.Join(d.path, pluginsDir)
}
