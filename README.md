## Guest Agent for Google Compute Engine

This repository contains the source code and packaging artifacts for the Google
guest agent and metadata script runner binaries. These components are installed
on Windows and Linux GCE VMs in order to enable GCE platform features.

**Table of Contents**

* [Overview](#overview)
* [Features](#features)
    * [Account Management](#account-management)
    * [Clock Skew](#clock-skew)
    * [OS Login](#os-login)
    * [Network](#network)
    * [Windows Failover Cluster Support](#windows-failover-cluster-support)
    * [Instance Setup](#instance-setup)
* [Metadata Scripts](#metadata-scripts)
* [Configuration](#configuration)
* [Packaging](#packaging)

## Overview

The repository contains these components:

*   **google-guest-agent** daemon which handles all of the areas outlined below
    in "features"
*   **google-metadata-script-runner** binary to run user-provided scripts at VM
    startup and shutdown.

## Features

The guest agent functionality can be separated into various areas of
responsibility. Historically, on Linux these were managed by separate
independent processes, but today they are all managed by the guest agent.

The `Daemons` section of the instance configs file on Linux refers to these
areas of responsibility. This allows a user to easily modify or disable
functionality. Behaviors for each area of responsibility are detailed below.

#### Account management

On Windows, the agent handles
[creating user accounts and setting/resetting passwords.](https://cloud.google.com/compute/docs/instances/windows/creating-passwords-for-windows-instances)

On Linux: If OS Login is not used, the guest agent will be responsible for
provisioning and deprovisioning user accounts. The agent creates local user
accounts and maintains the authorized SSH keys file for each. User account
creation is based on
[adding and remove SSH Keys](https://cloud.google.com/compute/docs/instances/adding-removing-ssh-keys)
stored in metadata.

The guest agent has the following behaviors:

*   Administrator permissions are managed with a `google-sudoers` Linux group.
    Members of this group are granted `sudo` permissions on the VM.
*   All users provisioned by the account daemon are added to the
    `google-sudoers` group.
*   The daemon stores a file in the guest to record which user accounts are
    managed by Google.
*   User accounts not managed by Google are not touched by the accounts daemon.
*   The authorized keys file for a Google managed user is deleted when all SSH
    keys for the user are removed from metadata.

#### OS Login

(Linux only)

If the user has
[configured OS Login via metadata](https://cloud.google.com/compute/docs/instances/managing-instance-access),
the guest agent will be responsible for configuring the OS to use OS Login,
otherwise called 'enabling' OS Login. This consists of:

* Adding a Google config block to the SSHD configuration file and restarting
  SSHD.
* Adding OS Login entries to the nsswitch.conf file.
* Adding OS Login entries to the PAM configuration file for SSHD.

If the user disables OS login via metadata, the configuration changes will be
removed.

#### Clock Skew

(Linux only)

The guest agent is responsible for syncing the software clock with the
hypervisor clock after a stop/start event or after a migration. Preventing clock
skew may result in `system time has changed` messages in VM logs.

#### Network

The guest agent uses network interface metadata to manage the network
interfaces in the guest by performing the following tasks:

*   Enabled all associated network interfaces on boot.
*   Setup or remove IP routes in the guest for IP forwarding and IP aliases
    *   Only IPv4 IP addresses are currently supported.
    *   Routes are set on the primary ethernet interface.
    *   Google routes are configured, by default, with the routing protocol ID
        `66`. This ID is a namespace for daemon configured IP addresses. It can
        be changed with the config file, see below.

#### Windows Failover Cluster Support

(Windows only)

The agent can monitor the active node in the Windows Failover Cluster and
coordinate with GCP Internal Load Balancer to forward all cluster traffic to the
expected node.

The following fields on instance metadata or instance\_configs.cfg can control
the behavior:

* `enable-wsfc`: If set to true, all IP forwarding info will be ignored and
  agent will start responding to the health check port. Default false.
* `wsfc-agent-port`: The port which the agent will respond to health checks.
  Default 59998.
* `wsfc-addrs`: A comma separated list of IP address. This is an advanced
  setting to enable user have both normal forwarding IPs and cluster IPs on the
  same instance. If set, agent will only skip-auto configuring IPs in the list.
  Default empty.

#### Instance Setup

(Linux only)

The guest agent will perform some actions once each time on startup:

*   Optimize for local SSD.
*   Enable multi-queue on all the virtionet devices.

The guest agent will perform some actions one time only, on the first VM boot:

*   Generate SSH host keys.
*   Create the `boto` config for using Google Cloud Storage.

## Metadata Scripts

Metadata scripts implement support for running user provided
[startup scripts](https://cloud.google.com/compute/docs/startupscript) and
[shutdown scripts](https://cloud.google.com/compute/docs/shutdownscript). The
guest support for metadata scripts is implemented in Python with the following
design details.

*   Metadata scripts are executed in a shell.
*   If multiple metadata keys are specified (e.g. `startup-script` and
    `startup-script-url`) both are executed.
*   If multiple metadata keys are specified (e.g. `startup-script` and
    `startup-script-url`) a URL is executed first.
*   The exit status of a metadata script is logged after completed execution.

## Configuration

Users of Google provided images may configure the guest environment behaviors
using a configuration file.

To make configuration changes on Windows, follow
[these instructions](https://cloud.google.com/compute/docs/instances/windows/creating-managing-windows-instances#configure-windows-features)

To make configuration changes on Linux, add settings to
`/etc/default/instance_configs.cfg`. If you are attempting to change
the behavior of a running instance, restart the guest agent after modifying.

Linux distributions looking to include their own defaults can specify settings
in `/etc/default/instance_configs.cfg.distro`. These settings will not override
`/etc/default/instance_configs.cfg`. This enables distribution settings that do
not override user configuration during package update.

The following are valid user configuration options.

Section           | Option                 | Value
----------------- | ---------------------- | -----
Accounts          | deprovision\_remove    | `true` makes deprovisioning a user destructive.
Accounts          | groups                 | Comma separated list of groups for newly provisioned users.
Accounts          | useradd\_cmd           | Command string to create a new user.
Accounts          | userdel\_cmd           | Command string to delete a user.
Accounts          | usermod\_cmd           | Command string to modify a user's groups.
Accounts          | gpasswd\_add\_cmd      | Command string to add a user to a group.
Accounts          | gpasswd\_remove\_cmd   | Command string to remove a user from a group.
Accounts          | groupadd\_cmd          | Command string to create a new group.
Daemons           | accounts\_daemon       | `false` disables the accounts daemon.
Daemons           | clock\_skew\_daemon    | `false` disables the clock skew daemon.
Daemons           | network\_daemon        | `false` disables the network daemon.
InstanceSetup     | host\_key\_types       | Comma separated list of host key types to generate.
InstanceSetup     | optimize\_local\_ssd   | `false` prevents optimizing for local SSD.
InstanceSetup     | network\_enabled       | `false` skips instance setup functions that require metadata.
InstanceSetup     | set\_boto\_config      | `false` skips setting up a `boto` config.
InstanceSetup     | set\_host\_keys        | `false` skips generating host keys on first boot.
InstanceSetup     | set\_multiqueue        | `false` skips multiqueue driver support.
IpForwarding      | ethernet\_proto\_id    | Protocol ID string for daemon added routes.
IpForwarding      | ip\_aliases            | `false` disables setting up alias IP routes.
IpForwarding      | target\_instance\_ips  | `false` disables internal IP address load balancing.
MetadataScripts   | default\_shell         | String with the default shell to execute scripts.
MetadataScripts   | run\_dir               | String base directory where metadata scripts are executed.
MetadataScripts   | startup                | `false` disables startup script execution.
MetadataScripts   | shutdown               | `false` disables shutdown script execution.
NetworkInterfaces | setup                  | `false` skips network interface setup.
NetworkInterfaces | ip\_forwarding         | `false` skips IP forwarding.
NetworkInterfaces | dhcp\_command          | String path for alternate dhcp executable used to enable network interfaces.

Setting `network_enabled` to `false` will disable generating host keys and the
`boto` config in the guest.

## Packaging

The guest agent and metadata script runner are packaged in DEB, RPM or Googet
format packages which are published to Google Cloud repositories and
preinstalled on Google managed GCE Images. Packaging scripts for each platform
are stored in the packaging/ directory.

We build the following packages for the Windows guest environment:

google-compute-engine-windows - contains the guest agent executable.
google-compute-engine-metadata-scripts - contains files to run startup and shutdown scripts.

We build the following packages for the Linux guest environment:

google-guest-agent - contains the guest agent and metadata script runner
executables, as well as service files for both.
