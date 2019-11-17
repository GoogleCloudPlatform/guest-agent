# Copyright 2018 Google Inc. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Don't build debuginfo packages.
%define debug_package %{nil}

Name: google-guest-agent
Epoch:   1
Version: %{_version}
Release: g1%{?dist}
Summary: Google Compute Engine guest agent.
License: ASL 2.0
Url: https://cloud.google.com/compute/docs/images/guest-environment
Source0: %{name}_%{version}.orig.tar.gz

BuildArch: %{_arch}
%if ! 0%{?el6}
BuildRequires: systemd
%endif

%description
Contains the Google guest agent binary.

%prep
%autosetup

%build
cd google_guest_agent
GOPATH=%{_gopath} CGO_ENABLED=0 %{_go} build -ldflags="-s -w -X main.version=%{_version}" -mod=readonly

%install
install -d %{buildroot}%{_bindir}
install -p -m 0755 google_guest_agent/google_guest_agent %{buildroot}%{_bindir}/google_guest_agent
install -d %{buildroot}/usr/share/google-guest-agent
install -p -m 0644 instance_configs.cfg %{buildroot}/usr/share/google-guest-agent/instance_configs.cfg
%if 0%{?el6}
install -d %{buildroot}/etc/init
install -p -m 0644 %{name}.conf %{buildroot}/etc/init
%else
install -d %{buildroot}%{_unitdir}
install -d %{buildroot}%{_presetdir}
install -p -m 0644 %{name}.service %{buildroot}%{_unitdir}
install -p -m 0644 90-%{name}.preset %{buildroot}%{_presetdir}/90-%{name}.preset
%endif

%files
%defattr(-,root,root,-)
%{_bindir}/google_guest_agent
%if 0%{?el6}
/etc/init/%{name}.conf
%else
%{_unitdir}/%{name}.service
%{_presetdir}/90-%{name}.preset
%endif

%if ! 0%{?el6}

%post
%systemd_post google-guest-agent.service
if [ $1 -eq 1 ]; then
  if [ ! -f /etc/default/instance_configs.cfg ]; then
    cp -a /usr/share/google-guest-agent/instance_configs.cfg /etc/default/
  fi
fi

%preun
%systemd_preun google-guest-agent.service

%postun
%systemd_postun google-guest-agent.service
if [ $1 -eq 1 ]; then
  if [ -f /etc/default/instance_configs.cfg ]; then
    rm /etc/default/instance_configs.cfg
  fi
fi

%endif
