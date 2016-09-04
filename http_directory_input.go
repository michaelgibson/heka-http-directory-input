package http

import (
	"errors"
	"fmt"
	"github.com/bbangert/toml"
	. "github.com/mozilla-services/heka/pipeline"
	"github.com/mozilla-services/heka/plugins/http"
	"os"
	"path/filepath"
)

type HttpEntry struct {
	ir       InputRunner
	maker    MutableMaker
	config   *http.HttpInputConfig
	fileName string
}

type HttpDirectoryInputConfig struct {
	// Root folder of the tree where the scheduled jobs are defined.
	HttpDir string `toml:"http_dir"`

	// Number of seconds to wait between scans of the job directory. Defaults
	// to 300.
	TickerInterval uint `toml:"ticker_interval"`
}

type HttpDirectoryInput struct {
	// The actual running InputRunners.
	inputs map[string]*HttpEntry
	// Set of InputRunners that should exist as specified by walking
	// the Http directory.
	specified map[string]*HttpEntry
	stopChan  chan bool
	logDir    string
	ir        InputRunner
	h         PluginHelper
	pConfig   *PipelineConfig
}

// Helper function for manually comparing structs since slice attributes mean
// we can't use `==`.
func (lsdi *HttpDirectoryInput) Equals(runningEntry *http.HttpInputConfig, otherEntry *http.HttpInputConfig) bool {
	if runningEntry.Url != otherEntry.Url {
		return false
	}
  if len(runningEntry.Urls) != len(otherEntry.Urls) {
    return false
  }
  for i, v := range runningEntry.Urls {
    if otherEntry.Urls[i] != v {
      return false
    }
  }
	if runningEntry.Method != otherEntry.Method {
		return false
	}
  if len(runningEntry.Headers) != len(otherEntry.Headers) {
    return false
  }
  for i, v := range runningEntry.Headers {
    if otherEntry.Headers[i] != v {
      return false
    }
  }
	if runningEntry.Body != otherEntry.Body {
		return false
	}
	if runningEntry.Username != otherEntry.Username {
		return false
	}
  if runningEntry.Password != otherEntry.Password {
		return false
	}
  if runningEntry.TickerInterval != otherEntry.TickerInterval {
    return false
  }
  if runningEntry.SuccessSeverity != otherEntry.SuccessSeverity {
		return false
	}
  if runningEntry.ErrorSeverity != otherEntry.ErrorSeverity {
		return false
	}
	return true
}

// Heka will call this before calling any other methods to give us access to
// the pipeline configuration.
func (lsdi *HttpDirectoryInput) SetPipelineConfig(pConfig *PipelineConfig) {
	lsdi.pConfig = pConfig
}

func (lsdi *HttpDirectoryInput) Init(config interface{}) (err error) {
	conf := config.(*HttpDirectoryInputConfig)
	lsdi.inputs = make(map[string]*HttpEntry)
	lsdi.stopChan = make(chan bool)
	globals := lsdi.pConfig.Globals
	lsdi.logDir = filepath.Clean(globals.PrependShareDir(conf.HttpDir))
	return
}

// ConfigStruct implements the HasConfigStruct interface and sets defaults.
func (lsdi *HttpDirectoryInput) ConfigStruct() interface{} {
	return &HttpDirectoryInputConfig{
		HttpDir: "http.d",
		TickerInterval: 300,
	}
}

func (lsdi *HttpDirectoryInput) Stop() {
	close(lsdi.stopChan)
}

// CleanupForRestart implements the Restarting interface.
func (lsdi *HttpDirectoryInput) CleanupForRestart() {
	lsdi.Stop()
}

func (lsdi *HttpDirectoryInput) Run(ir InputRunner, h PluginHelper) (err error) {
	lsdi.ir = ir
	lsdi.h = h
	if err = lsdi.loadInputs(); err != nil {
		return
	}

	var ok = true
	ticker := ir.Ticker()

	for ok {
		select {
		case _, ok = <-lsdi.stopChan:
		case <-ticker:
			if err = lsdi.loadInputs(); err != nil {
				return
			}
		}
	}

	return
}

// Reload the set of running HttpInput InputRunners. Not reentrant, should
// only be called from the HttpDirectoryInput's main Run goroutine.
func (lsdi *HttpDirectoryInput) loadInputs() (err error) {
	dups := false
	var runningEntryInputName string

	// Clear out lsdi.specified and then populate it from the file tree.
	lsdi.specified = make(map[string]*HttpEntry)
	if err = filepath.Walk(lsdi.logDir, lsdi.logDirWalkFunc); err != nil {
		return
	}

	// Remove any running inputs that are no longer specified
	for name, entry := range lsdi.inputs {
		if _, ok := lsdi.specified[name]; !ok {
			lsdi.pConfig.RemoveInputRunner(entry.ir)
			delete(lsdi.inputs, name)
			lsdi.ir.LogMessage(fmt.Sprintf("Removed: %s", name))
		}
	}

	// Iterate through the specified inputs and activate any that are new or
	// have been modified.

	for name, newEntry := range lsdi.specified {

		//Check to see if duplicate input already exists with same name but different file location.
		//If so, do not load it as it confuses the InputRunner
		for runningInputName, runningInput := range lsdi.inputs {
			if newEntry.ir.Name() == runningInput.ir.Name() && newEntry.fileName != runningInput.fileName {
				runningEntryInputName = runningInput.ir.Name()
				dups = true
				lsdi.pConfig.RemoveInputRunner(runningInput.ir)
				lsdi.ir.LogMessage(fmt.Sprintf("Removed: %s", runningInputName))
				delete(lsdi.inputs, runningInputName)
				return fmt.Errorf("Duplicate Name: Input with name [%s] already exists. Not loading input file: %s", runningEntryInputName, name)
			}
		}

		if runningEntry, ok := lsdi.inputs[name]; ok {
			if (lsdi.Equals(runningEntry.config, newEntry.config) && runningEntry.ir.Name() == newEntry.ir.Name()) && !dups {
				// Nothing has changed, let this one keep running.
				continue
			}
			// It has changed, stop the old one.
			lsdi.pConfig.RemoveInputRunner(runningEntry.ir)
			lsdi.ir.LogMessage(fmt.Sprintf("Removed: %s", name))
			delete(lsdi.inputs, name)
		}

		// Start up a new input.
		if err = lsdi.pConfig.AddInputRunner(newEntry.ir); err != nil {
			lsdi.ir.LogError(fmt.Errorf("creating input '%s': %s", name, err))
			continue
		}
		lsdi.inputs[name] = newEntry
		lsdi.ir.LogMessage(fmt.Sprintf("Added: %s", name))
	}
	return
}

// Function of type filepath.WalkFunc, called repeatedly when we walk a
// directory tree using filepath.Walk. This function is not reentrant, it
// should only ever be triggered from the similarly not reentrant loadInputs
// method.
func (lsdi *HttpDirectoryInput) logDirWalkFunc(path string, info os.FileInfo,
	err error) error {

	if err != nil {
		lsdi.ir.LogError(fmt.Errorf("walking '%s': %s", path, err))
		return nil
	}
	// info == nil => filepath doesn't actually exist.
	if info == nil {
		return nil
	}
	// Skip directories or anything that doesn't end in `.toml`.
	if info.IsDir() || filepath.Ext(path) != ".toml" {
		return nil
	}

	// Things look good so far. Try to load the data into a config struct.
	var entry *HttpEntry
	if entry, err = lsdi.loadHttpFile(path); err != nil {
		lsdi.ir.LogError(fmt.Errorf("loading http file '%s': %s", path, err))
		return nil
	}

	// Override the config settings we manage, make the runner, and store the
	// entry.
	prepConfig := func() (interface{}, error) {
		config, err := entry.maker.OrigPrepConfig()
		if err != nil {
			return nil, err
		}
		httpInputConfig := config.(*http.HttpInputConfig)
		return httpInputConfig, nil
	}
	config, err := prepConfig()
	if err != nil {
		lsdi.ir.LogError(fmt.Errorf("prepping config: %s", err.Error()))
		return nil
	}
	entry.config = config.(*http.HttpInputConfig)
	entry.maker.SetPrepConfig(prepConfig)

	runner, err := entry.maker.MakeRunner("")
	if err != nil {
		lsdi.ir.LogError(fmt.Errorf("making runner: %s", err.Error()))
		return nil
	}

	entry.ir = runner.(InputRunner)
	entry.ir.SetTransient(true)
	entry.fileName = path
	lsdi.specified[path] = entry
	return nil
}

func (lsdi *HttpDirectoryInput) loadHttpFile(path string) (*HttpEntry, error) {
	var (
		err     error
		ok      = false
		section toml.Primitive
	)

	unparsedConfig := make(map[string]toml.Primitive)
	if _, err = toml.DecodeFile(path, &unparsedConfig); err != nil {
		return nil, err
	}
	for name, conf := range unparsedConfig {
		confName, confType, _ := lsdi.getConfigFileInfo(name, conf)
		if confType == "HttpInput" {
			ok = true
			section = conf
			path = confName
			continue
		}
	}

	if !ok {
		err = errors.New("No `HttpInput` section.")
		return nil, err
	}

	maker, err := NewPluginMaker("HttpInput", lsdi.pConfig, section)
	if err != nil {
		return nil, fmt.Errorf("can't create plugin maker: %s", err)
	}

	mutMaker := maker.(MutableMaker)
	mutMaker.SetName(path)

	prepCommonTypedConfig := func() (interface{}, error) {
		commonTypedConfig, err := mutMaker.OrigPrepCommonTypedConfig()
		if err != nil {
			return nil, err
		}
		commonInput := commonTypedConfig.(CommonInputConfig)
		commonInput.Retries = RetryOptions{
			MaxDelay:   "30s",
			Delay:      "250ms",
			MaxRetries: -1,
		}
		if commonInput.CanExit == nil {
			b := true
			commonInput.CanExit = &b
		}
		return commonInput, nil
	}
	mutMaker.SetPrepCommonTypedConfig(prepCommonTypedConfig)

	entry := &HttpEntry{
		maker: mutMaker,
	}
	return entry, nil
}

func (lsdi *HttpDirectoryInput) getConfigFileInfo(name string, configFile toml.Primitive) (configName string, configType string, configCategory string) {
	//Get identifiers from config section
	pipeConfig := NewPipelineConfig(nil)
	maker, _ := NewPluginMaker(name, pipeConfig, configFile)
	if maker.Type() != "" {
		return maker.Name(), maker.Type(), maker.Category()
	}
	return "", "", ""
}

func init() {
	RegisterPlugin("HttpDirectoryInput", func() interface{} {
		return new(HttpDirectoryInput)
	})
}
