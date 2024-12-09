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
    * [Telemetry](#telemetry)
    * [MTLS MDS](#mtls-mds)
* [Metadata Scripts](#metadata-scripts)
* [Configuration](#configuration)
* [Packaging](#packaging)

## Overview

The repository contains these components:

*   **google-guest-agent** daemon which handles all of the areas outlined below
    in "features"
*   **google-metadata-script-runner** binary to run user-provided scripts at VM
    startup and shutdown.

Note: Starting in December 2024 guest agent installs a secondary systemd unit
named google-guest-agent-manager, this unit manages on demand plugins lifecycle,
such plugin architecture will support expansion of the guest agent in the future.
In the upcoming releases both google-guest-agent and google-guest-agent-manager
will be consolidated in a single unit. The source code for plugin manager can
be found [here](https://github.com/GoogleCloudPlatform/google-guest-agent).

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

Guest Agent automatically creates local user accounts for any SSH user defined
in the Metadata SSH keys at the instance or project level (unless blocked) 
on Windows instances to support [connecting to Windows VMs using SSH.](https://cloud.google.com/compute/docs/connect/windows-ssh)

> Active Directory Domain Controller does not use the local user account database
except when it is booted into the recovery console or demoted, so any account 
created on the system would become an administrator of the Active Directory Domain.
You can prevent unintended AD user provisioning by [disabling the account manager](https://cloud.google.com/compute/docs/instances/windows/creating-managing-windows-instances#disable_the_account_manager) on the AD controller VM.
Refer [deploy domain controllers](https://cloud.google.com/architecture/deploy-an-active-directory-forest-on-compute-engine#deploy_domain_controllers) for more information
on setting up AD on GCE.

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
*   User accounts not managed by the agent are not touched by the accounts daemon.
*   The authorized keys file for a Google managed user is deleted when all SSH
    keys for the user are removed from metadata.
*   Users accounts managed by the agent will be added to the `groups` config
    line in the `Accounts` section. If these groups do not exist, the agent
    will not create them.

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

Note that options under the `Accounts` section of the configuration do not apply
to oslogin users.

#### Clock Skew

(Linux only)

The guest agent is responsible for syncing the software clock with the
hypervisor clock after a stop/start event or after a migration. Preventing clock
skew may result in `system time has changed` messages in VM logs.

#### Network

The guest agent uses network interface metadata to manage the network
interfaces by performing the following tasks:

*   Enable all associated network interfaces on boot.
    *   Detect the current active network manager service that is managing the
        primary NIC.
    *   Rollback and delete any guest agent-managed files/configurations left
        behind by all other supported network manager services.
    *   Write and apply new configurations for the secondary NICs using the
        network manager service detected in the first step.
    *   Create a route to the metadata server for the primary NIC only.
*   Setup or remove IP routes in the guest for IP forwarding and IP aliases
    *   Only IPv4 IP addresses are currently supported.
    *   Routes are set on the primary ethernet interface.
    *   Google routes are configured, by default, with the routing protocol ID
        `66`. This ID is a namespace for daemon configured IP addresses. It can
        be changed with the config file, see below.

On Linux, supported network managers are as follows. These are listed by
descending priority and include the location at which the configuration files
are written.

*   `netplan`
    *   Config location: `/run/netplan/`
        *   ex: `/run/netplan/20-google-guest-agent-eth0.yaml`
    *   Dropin location: `/etc/systemd/network/`
        *   ex: `/etc/systemd/network/10-netplan-eth0.network.d/`
*   `wicked`
    *   Config location: `/etc/sysconfig/network/`
        *   ex: `/etc/sysconfig/network/ifcfg-eth0`
    *   Notes:
        *   Existing `ifcfg` files are not overwritten and are skipped instead.
*   `NetworkManager`
    *   Config location: `/etc/NetworkManager/system-connections/`
        *   ex:
            `/etc/NetworkManager/system-connections/google-guest-agent-eth0.nmconnection`
*   `systemd-networkd`
    *   Config location: `/usr/lib/systemd/network/`
        *   ex: `/usr/lib/systemd/network/20-eth0-google-guest-agent.network`
*   `dhclient`
    *   Config location: `/run/`
        *   ex (pid):   `/run/dhclient.google-guest-agent.eth0.ipv4.pid`
        *   ex (lease): `/run/dhclient.google-guest-agent.eth0.ipv4.lease`
    *   Notes:
        *   The primary NIC setup, if enabled, is skipped if a dhclient process
            for the primary NIC is already running.

If none of the first 4 network manager services are detected on the system, then
the agent will default to using `dhclient` for managing network interfaces.

Note: Ubuntu 18.04, while having `netplan` installed,  ships a outdated and 
unsupported version of `networkctl`. This older version lacks essential commands like 
`networkctl reload`, causing compatibility issues. Guest agent is designed to 
fallback to dhclient on Ubuntu 18.04, even when netplan is present, to ensure proper
network configuration.

The following configuration flags can control the behavior:

*   `manage_primary_nic`: When enabled, the agent will start managing the
    primary NIC in addition to the secondary NICs.

For more information about the instance configuration, see the Configuration
section.

The guest agent will also setup VLANs if VLAN is enabled. The setup and
configuration for this work similarly to the normal NIC configuration.

If the VLANs' parent interface is the primary NIC, it will apply the VLAN
configurations regardless of whether `manage_primary_nic` is set.

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

#### Telemetry

The guest agent will record some basic system telemetry information at start and
then once every 24 hours. 

*   Guest agent version and architecture
*   Operating system name and version
*   Operating system kernel release and version

Telemetry can be disabled by setting the metadata key `disable-guest-telemetry`
to `true`.

#### MTLS MDS

GCE [Shielded VMs](https://cloud.google.com/compute/shielded-vm/docs/shielded-vm)
now support HTTPS endpoint `https://metadata.google.internal/computeMetadata/v1`
for Metadata Server. To enable communication with secure HTTPS endpoint, Guest Agent 
retrieves and stores credentials on the VM's disk in a standard location, making them 
accessible to any client application running on the VM. Both the root certificate 
and client credentials are updated each time the guest-agent process starts. 
For enhanced security, client credentials are automatically refreshed every 48 hours.
The agent generates and saves new credentials, while the old ones remain valid. 
This overlap period ensures that clients have sufficient time to transition
to the new credentials before the old ones expire, and it allows the agent to retry in case
of failure and obtain valid credentials before the existing ones become invalid. Client 
credentials are basically EC private key and the client certificate concatenated. These 
credentials are unique to an instance and would not work elsewhere.

Refer [this](https://cloud.google.com/compute/docs/metadata/overview#https-mds) 
for more information on HTTPS metadata server endpoint and credential details 
including their lifespan.

Credentials can be stored at these supported locations - 

* Linux: 

    - Client credentials: `/run/google-mds-mtls/client.key`
    - Root certificate: `/run/google-mds-mtls/root.crt` and local trust store based on 
    target OS. Refer [this](https://cloud.google.com/compute/docs/metadata/overview#https-mds-certificates) 
    for local trust store location for each target OS.

* Windows:

    - Client credentials: `C:\ProgramData\Google\ComputeEngine\mds-mtls-client.key` and 
    `Cert:\LocalMachine\My`
    - Root certificate: `C:\ProgramData\Google\ComputeEngine\mds-mtls-root.crt` and 
    `Cert:\LocalMachine\Root`
    - [PFX](https://learn.microsoft.com/en-us/windows-hardware/drivers/install/personal-information-exchange---pfx--files): `C:\ProgramData\Google\Compute Engine\mds-mtls-client.key.pfx`

    *Credentials can be stored on disk as well as in [Certificate Store](https://learn.microsoft.com/en-us/windows-hardware/drivers/install/certificate-stores) on Windows*

Note that this is disabled by default, if HTTPS endpoint is supported on a VM, the feature
can be enabled by setting `disable-https-mds-setup = false` under `[MDS]` section
in `instance_configs.cfg` file. 

If enabled, agent will write certificates only on disk by default and users can
opt-in to have certificates in OS Native stores. This means in case of Linux based VMs
MDS Root certificate will be added to trust store like 
`/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem` on RHEL based systems 
and `/etc/ssl/certs/ca-certificates.crt` on Debian based. Local root trust store 
is updated by running `update-ca-certificates` or `update-ca-trust` tool based on the OS.
On Windows, Client credentials will be added in `Cert:\LocalMachine\My` and Root
certificate in `Cert:\LocalMachine\Root`. This can be enabled by setting `enable-https-mds-native-cert-store = true` under same `[MDS]` section.

> As documented by Microsoft [here](https://learn.microsoft.com/en-us/troubleshoot/windows-server/active-directory/enable-ldap-over-ssl-3rd-certification-authority#possible-issues) there
could be issues with LDAPS process when multiple certificates are added in personal
store. Avoid enabling OS Native stores on Domain Controllers. Credentials can still
be used from disk if required.


## Metadata Scripts

Metadata scripts implement support for running user provided
[startup scripts](https://cloud.google.com/compute/docs/startupscript) and
[shutdown scripts](https://cloud.google.com/compute/docs/shutdownscript). The
guest support for metadata scripts consider the following details:

*   Metadata scripts are executed in a shell.
*   If multiple metadata keys are specified (e.g. `startup-script` and
    `startup-script-url`) both are executed.
*   If multiple metadata keys are specified (e.g. `startup-script` and
    `startup-script-url`) a URL is executed first.
*   The exit status of a metadata script is logged after completed execution.

For Windows specific details refer to: [Use startup scripts on Windows VMs](https://cloud.google.com/compute/docs/instances/startup-scripts/windows).

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
Accounts          | groups                 | Comma separated list of groups for newly provisioned users created from metadata ssh keys.
Accounts          | useradd\_cmd           | Command string to create a new user.
Accounts          | userdel\_cmd           | Command string to delete a user.
Accounts          | usermod\_cmd           | Command string to modify a user's groups.
Accounts          | gpasswd\_add\_cmd      | Command string to add a user to a group.
Accounts          | gpasswd\_remove\_cmd   | Command string to remove a user from a group.
Accounts          | groupadd\_cmd          | Command string to create a new group.
Core              | cloud\_logging\_enabled| `false` disable cloud logging.
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
NetworkInterfaces | manage\_primary\_nic   | `true` will start managing the primary NIC in addition to the secondary NICs.
NetworkInterfaces | dhcp\_command          | String path for alternate dhcp executable used to enable network interfaces.
NetworkInterfaces | restore_debian12_netplan_config | `true` will create the debian-12's default netplan  configuration. It's set `true` by default.
OSLogin           | cert_authentication    | `false` prevents guest-agent from setting up sshd's `TrustedUserCAKeys`, `AuthorizedPrincipalsCommand` and `AuthorizedPrincipalsCommandUser` configuration keys. Default value: `true`.

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

google-compute-engine-windows contains `GCEAgent` and `GCEAgentManager` windows services.

We build the following packages for the Linux guest environment:

google-guest-agent - contains the guest agent, google-guest-agent-manager and metadata script runner
executables, as well as service files for all.

Refer [this](https://github.com/GoogleCloudPlatform/google-guest-agent) repo for further details on
Google Guest Agent Manager.
