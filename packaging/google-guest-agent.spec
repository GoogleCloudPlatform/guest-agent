#  Copyright 2018 Google LLC

#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at

#     https://www.apache.org/licenses/LICENSE-2.0

#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.

# Don't build debuginfo packages.
%define debug_package %{nil}

# The only use for extra source is to build plugin manager.
%if 0%{?has_extra_source}
%define build_plugin_manager %{has_extra_source}
%endif

Name: google-guest-agent
Epoch:   1
Version: %{_version}
Release: g1%{?dist}
Summary: Google Compute Engine guest agent.
License: ASL 2.0
Url: https://cloud.google.com/compute/docs/images/guest-environment
Source0: %{name}_%{version}.orig.tar.gz

%if 0%{?build_plugin_manager}
Source1: %{name}_extra-%{version}.orig.tar.gz
%endif

Requires: google-compute-engine-oslogin >= 1:20231003

BuildArch: %{_arch}
%if ! 0%{?el6}
BuildRequires: systemd
%endif

Obsoletes: python-google-compute-engine, python3-google-compute-engine

%description
Contains the Google guest agent binary.

%prep

%if 0%{?build_plugin_manager}
%autosetup -a 1
%else
%autosetup
%endif

%build
for bin in google_guest_agent google_metadata_script_runner gce_workload_cert_refresh; do
  pushd "$bin"
  GOPATH=%{_gopath} CGO_ENABLED=0 %{_go} build -ldflags="-s -w -X main.version=%{_version}" -mod=readonly
  popd
done
# Build side-by-side both new agent (plugin manager) and legacy agent.
%if 0%{?build_plugin_manager}
pushd %{name}-extra-%{version}/
  VERSION=%{version} make cmd/google_guest_agent/google_guest_agent
  VERSION=%{version} make cmd/ggactl/ggactl_plugin
  VERSION=%{version} make cmd/google_guest_compat_manager/google_guest_compat_manager
  VERSION=%{version} make cmd/core_plugin/core_plugin
  VERSION=%{version} make cmd/gce_metadata_script_runner/gce_metadata_script_runner
  VERSION=%{version} make cmd/metadata_script_runner_compat/gce_compat_metadata_script_runner
popd
%endif

%install
install -d "%{buildroot}/%{_docdir}/%{name}"
cp -r THIRD_PARTY_LICENSES "%buildroot/%_docdir/%name/THIRD_PARTY_LICENSES"

install -d %{buildroot}%{_bindir}
install -p -m 0755 google_guest_agent/google_guest_agent %{buildroot}%{_bindir}/google_guest_agent
install -p -m 0755 google_metadata_script_runner/google_metadata_script_runner %{buildroot}%{_bindir}/google_metadata_script_runner
install -p -m 0755 google_metadata_script_runner_adapt %{buildroot}%{_bindir}/google_metadata_script_runner_adapt
install -p -m 0755 gce_workload_cert_refresh/gce_workload_cert_refresh %{buildroot}%{_bindir}/gce_workload_cert_refresh
install -d %{buildroot}/usr/share/google-guest-agent
install -p -m 0644 instance_configs.cfg %{buildroot}/usr/share/google-guest-agent/instance_configs.cfg

# Compat agent, it will become google_guest_agent after the full package transition.
%if 0%{?build_plugin_manager}
install -d %{buildroot}%{_exec_prefix}/lib/google/guest_agent
install -p -m 0755 %{name}-extra-%{version}/cmd/gce_metadata_script_runner/gce_metadata_script_runner %{buildroot}%{_bindir}/gce_metadata_script_runner
install -p -m 0755 %{name}-extra-%{version}/cmd/google_guest_agent/google_guest_agent %{buildroot}%{_bindir}/google_guest_agent_manager
install -p -m 0755 %{name}-extra-%{version}/cmd/ggactl/ggactl_plugin %{buildroot}%{_bindir}/ggactl_plugin
install -p -m 0755 %{name}-extra-%{version}/cmd/google_guest_compat_manager/google_guest_compat_manager %{buildroot}%{_bindir}/google_guest_compat_manager
install -p -m 0755 %{name}-extra-%{version}/cmd/core_plugin/core_plugin %{buildroot}%{_exec_prefix}/lib/google/guest_agent/core_plugin
install -p -m 0755 %{name}-extra-%{version}/cmd/metadata_script_runner_compat/gce_compat_metadata_script_runner %{buildroot}%{_bindir}/gce_compat_metadata_script_runner

# Dispatcher hook route setup.
install -d /usr/lib/networkd-dispatcher/routable.d
install -p -m 0755 %{name}-extra-%{version}/build/configs/google_guest_agent_routes_setup.sh %{buildroot}/usr/lib/networkd-dispatcher/routable.d/google_guest_agent_routes_setup.sh
install -d /etc/NetworkManager/dispatcher.d
install -p -m 0755 %{name}-extra-%{version}/build/configs/google_guest_agent_routes_setup.sh %{buildroot}/etc/NetworkManager/dispatcher.d/google_guest_agent_routes_setup.sh
install -d /etc/sysconfig/network/scripts
install -p -m 0755 %{name}-extra-%{version}/build/configs/google_guest_agent_routes_setup.sh %{buildroot}/etc/sysconfig/network/scripts/google_guest_agent_routes_setup.sh

%endif

%if 0%{?el6}
install -d %{buildroot}/etc/init
install -p -m 0644 %{name}.conf %{buildroot}/etc/init/
install -p -m 0644 google-startup-scripts.conf %{buildroot}/etc/init/
install -p -m 0644 google-shutdown-scripts.conf %{buildroot}/etc/init/
%else
install -d %{buildroot}%{_unitdir}
install -d %{buildroot}%{_presetdir}
install -p -m 0644 %{name}.service %{buildroot}%{_unitdir}

%if 0%{?build_plugin_manager}
install -p -m 0644 google-guest-agent-manager.service %{buildroot}%{_unitdir}
install -p -m 0644 google-guest-compat-manager.service %{buildroot}%{_unitdir}
%endif

install -p -m 0644 google-startup-scripts.service %{buildroot}%{_unitdir}
install -p -m 0644 google-shutdown-scripts.service %{buildroot}%{_unitdir}
install -p -m 0644 gce-workload-cert-refresh.service %{buildroot}%{_unitdir}
install -p -m 0644 gce-workload-cert-refresh.timer %{buildroot}%{_unitdir}
install -p -m 0644 90-%{name}.preset %{buildroot}%{_presetdir}/90-%{name}.preset
%endif

%files
%{_docdir}/%{name}
%defattr(-,root,root,-)
/usr/share/google-guest-agent/instance_configs.cfg
%{_bindir}/google_guest_agent

%if 0%{?build_plugin_manager}
%{_bindir}/gce_metadata_script_runner
%{_bindir}/google_guest_compat_manager
%{_bindir}/gce_compat_metadata_script_runner
%{_bindir}/google_guest_agent_manager
%{_bindir}/ggactl_plugin
%{_exec_prefix}/lib/google/guest_agent/core_plugin

/usr/lib/networkd-dispatcher/routable.d/google_guest_agent_routes.sh
/etc/NetworkManager/dispatcher.d/google_guest_agent_routes.sh
/etc/sysconfig/network/scripts/google_guest_agent_routes.sh
%endif

%{_bindir}/google_metadata_script_runner
%{_bindir}/google_metadata_script_runner_adapt
%{_bindir}/gce_workload_cert_refresh
%if 0%{?el6}
/etc/init/%{name}.conf
/etc/init/google-startup-scripts.conf
/etc/init/google-shutdown-scripts.conf
%else
%{_unitdir}/%{name}.service

%if 0%{?build_plugin_manager}
%{_unitdir}/google-guest-agent-manager.service
%{_unitdir}/google-guest-compat-manager.service
%endif

%{_unitdir}/google-startup-scripts.service
%{_unitdir}/google-shutdown-scripts.service
%{_unitdir}/gce-workload-cert-refresh.service
%{_unitdir}/gce-workload-cert-refresh.timer
%{_presetdir}/90-%{name}.preset
%endif

%if ! 0%{?el6}
%post
if [ $1 -eq 1 ]; then
  # Initial installation

  # Install instance configs if not already present.
  if [ ! -f /etc/default/instance_configs.cfg ]; then
    cp -a /usr/share/google-guest-agent/instance_configs.cfg /etc/default/
  fi

  # Use enable instead of preset because preset is not supported in
  # chroots.
  systemctl enable google-guest-agent.service >/dev/null 2>&1 || :
  systemctl enable google-startup-scripts.service >/dev/null 2>&1 || :
  systemctl enable google-shutdown-scripts.service >/dev/null 2>&1 || :
  systemctl enable gce-workload-cert-refresh.timer >/dev/null 2>&1 || :

  %if 0%{?build_plugin_manager}
    systemctl enable google-guest-compat-manager.service >/dev/null 2>&1 || :
    systemctl enable google-guest-agent-manager.service >/dev/null 2>&1 || :
  %endif

  if [ -d /run/systemd/system ]; then
    systemctl daemon-reload >/dev/null 2>&1 || :
    systemctl start google-guest-agent.service >/dev/null 2>&1 || :
    systemctl start gce-workload-cert-refresh.timer >/dev/null 2>&1 || :
    %if 0%{?build_plugin_manager}
      systemctl start google-guest-compat-manager.service >/dev/null 2>&1 || :
      systemctl start google-guest-agent-manager.service >/dev/null 2>&1 || :
    %endif
  fi


else
  # Package upgrade
  %if 0%{?build_plugin_manager}
      systemctl enable google-guest-compat-manager.service >/dev/null 2>&1 || :
      systemctl enable google-guest-agent-manager.service >/dev/null 2>&1 || :
  %endif
    
  if [ -d /run/systemd/system ]; then
    systemctl daemon-reload >/dev/null 2>&1 || :
    systemctl try-restart google-guest-agent.service >/dev/null 2>&1 || :
    %if 0%{?build_plugin_manager}
      systemctl restart google-guest-compat-manager.service >/dev/null 2>&1 || :
      systemctl restart google-guest-agent-manager.service >/dev/null 2>&1 || :
    %endif
  fi

  # Re-enable the guest agent service if core plugin was enabled, since the
  # service would have been disabled, and stay disabled post-upgrade.
  if [ ! -f "/usr/bin/google_guest_compat_manager" ]; then
    if [ -f "/etc/google-guest-agent/core-plugin-enabled" ] && [ ! -z $(grep "true" "/etc/google-guest-agent/core-plugin-enabled") ]; then
      systemctl enable google-guest-agent.service > /dev/null 2>&1 || :
      systemctl enable gce-workload-cert-refresh.timer > /dev/null 2>&1 || :
    fi
  fi
fi

%preun
if [ $1 -eq 0 ]; then
  # Package removal, not upgrade
  %if 0%{?build_plugin_manager}
    systemctl --no-reload disable google-guest-compat-manager.service >/dev/null 2>&1 || :
    systemctl --no-reload disable google-guest-agent-manager.service >/dev/null 2>&1 || :
  %endif
  systemctl --no-reload disable google-guest-agent.service >/dev/null 2>&1 || :
  systemctl --no-reload disable google-startup-scripts.service >/dev/null 2>&1 || :
  systemctl --no-reload disable google-shutdown-scripts.service >/dev/null 2>&1 || :
  systemctl --no-reload disable gce-workload-cert-refresh.timer >/dev/null 2>&1 || :
  if [ -d /run/systemd/system ]; then
    systemctl stop google-guest-agent.service >/dev/null 2>&1 || :
    %if 0%{?build_plugin_manager}
      systemctl stop google-guest-compat-manager.service >/dev/null 2>&1 || :
      systemctl stop google-guest-agent-manager.service >/dev/null 2>&1 || :
      ggactl_plugin dynamic-cleanup >/dev/null 2>&1 || :
    %endif
  fi
fi

%postun
if [ $1 -eq 0 ]; then
  # Package removal, not upgrade

  if [ -f /etc/default/instance_configs.cfg ]; then
    rm /etc/default/instance_configs.cfg
  fi

  if [ -d /run/systemd/system ]; then
    systemctl daemon-reload >/dev/null 2>&1 || :
  fi
fi

%else

# EL6
%post
if [ $1 -eq 1 ]; then
  # Install instance configs if not already present.
  if [ ! -f /etc/default/instance_configs.cfg ]; then
    cp -a /usr/share/google-guest-agent/instance_configs.cfg /etc/default/
  fi

  # Initial installation
  initctl start google-guest-agent >/dev/null 2>&1 || :
else
  # Upgrade
  initctl restart google-guest-agent >/dev/null 2>&1 || :
fi

%preun
if [ $1 -eq 0 ]; then
  # Package removal, not upgrade
  initctl stop google-guest-agent >/dev/null 2>&1 || :
fi

%postun
if [ $1 -eq 0 ]; then
  # Package removal, not upgrade
  if [ -f /etc/default/instance_configs.cfg ]; then
    rm /etc/default/instance_configs.cfg
  fi
fi

%endif
