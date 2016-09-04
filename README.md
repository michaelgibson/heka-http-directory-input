Http Directory Input
=======================  
Dynamic plugin loader for Heka's HttpInput plugins

Plugin Name: **HttpDirectoryInput**

The HttpDirectoryInput is largely based on Heka's [ProcessDirectoryInput](https://hekad.readthedocs.io/en/latest/config/inputs/processdir.html).  
It periodically scans a filesystem directory looking
for HttpInput configuration files. The HttpDirectoryInput will maintain
a pool of running HttpInputs based on the contents of this directory,
refreshing the set of running inputs as needed with every rescan. This allows
Heka administrators to manage a set of HttpInputs for a running
hekad server without restarting the server.

Each HttpDirectoryInput has a `http_dir` configuration setting, which is
the root folder of the tree where scheduled jobs are defined.
This folder must contain TOML files which specify the details
regarding which HttpInput to run.

For example, a http_dir might look like this::


  - /usr/share/heka/https.d/
    - google.toml
    - nagios.toml
    - internal_app_rest_api.toml

The names for each Http input must be unique. Any duplicate named configs
will not be loaded.  
Ex.  

	[syslog]  
	type = "HttpInput"  
	and  
	[syslog2]  
	type = "HttpInput"


Each config file must have a '.toml' extension. Each file which meets these criteria,
such as those shown in the example above, should contain the TOML configuration for exactly one
[HttpInput](https://hekad.readthedocs.io/en/latest/config/inputs/http.html),
matching that of a standalone HttpInput with
the following restrictions:

- The section name OR type *must* be `HttpInput`. Any TOML sections named anything
  other than HttpInput will be ignored.


Config:

- ticker_interval (int, optional):
    Amount of time, in seconds, between scans of the http_dir. Defaults to
    300 (i.e. 5 minutes).
- http_dir (string, optional):
    This is the root folder of the tree where the scheduled jobs are defined.
    Absolute paths will be honored, relative paths will be computed relative to
    Heka's globally specified share_dir. Defaults to "http.d" (i.e.
    "$share_dir/http.d").
- retries (RetryOptions, optional):
    A sub-section that specifies the settings to be used for restart behavior
    of the HttpDirectoryInput (not the individual ProcessInputs, which are
    configured independently).
    See [Configuring Restarting Behavior](https://hekad.readthedocs.io/en/latest/config/index.html#configuring-restarting)

Example:

	[HttpDirectoryInput]
	http_dir = "/usr/share/heka/http.d"
	ticker_interval = 120

To Build
========

See [Building *hekad* with External Plugins](http://hekad.readthedocs.org/en/latest/installing.html#build-include-externals)
for compiling in plugins.

Edit cmake/plugin_loader.cmake file and add

    add_external_plugin(git https://github.com/michaelgibson/heka-http-directory-input master)

Build Heka:
  	. ./build.sh
