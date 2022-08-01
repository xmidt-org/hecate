# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](http://keepachangelog.com/en/1.0.0/)
and this project adheres to [Semantic Versioning](http://semver.org/spec/v2.0.0.html).

## [Unreleased]
- Dependency update, note vulnerabilities
  - [github.com/prometheus/client_golang v1.12.2 CVE-2022-21698 no patch available](https://github.com/advisories/GHSA-cg3q-j54f-5p7p)
  - [ github.com/pkg/sftp no patch available.](github.com/pkg/sftp)
	  - https://ossindex.sonatype.org/vulnerability/sonatype-2021-3619
  - [ golang.org/x/text no patch available.](golang.org/x/text)
	  - https://ossindex.sonatype.org/vulnerability/CVE-2021-38561?component-type=golang&component-name=golang.org%2Fx%2Ftext&utm_source=nancy-client&utm_medium=integration&utm_content=1.0.21

## [v0.2.0]
- Updated spec file and rpkg version macro to be able to choose when the 'v' is included in the version. [#32](https://github.com/xmidt-org/hecate/pull/32)
- Updated chrysom version, added ancla dependency.  Removed default owner value. [#36](https://github.com/xmidt-org/hecate/pull/36)

## [v0.1.2]
- Make owner and bucket configurable. [#26](https://github.com/xmidt-org/hecate/pull/26)
- Fix sonar badge in readme. [#29](https://github.com/xmidt-org/hecate/pull/29)
- Bump argus dep version. [#31](https://github.com/xmidt-org/hecate/pull/31)

## [v0.1.1]
- Use UberFx. [#20](https://github.com/xmidt-org/hecate/pull/20)
- Add helper servers such as metrics, health and pprof. [#23](https://github.com/xmidt-org/hecate/pull/23)

### Fixed
- Add missing systemd config. [#19](https://github.com/xmidt-org/hecate/pull/19)

## [v0.1.0]
- Migrate to github actions, normalize analysis tools, Dockerfiles and Makefiles. [#3](https://github.com/xmidt-org/hecate/pull/3)
- Add docker integration. [#7](https://github.com/xmidt-org/hecate/pull/7)
- Update buildtime format in Makefile to match RPM spec file. [#10](https://github.com/xmidt-org/hecate/pull/10)
- Initial run of the application. [#8](https://github.com/xmidt-org/hecate/pull/8)
- Sonarcube fix. [#16](https://github.com/xmidt-org/hecate/pull/16)

[Unreleased]: https://github.com/xmidt-org/hecate/compare/v0.2.0...HEAD
[v0.2.0]: https://github.com/xmidt-org/hecate/compare/v0.1.2...v0.2.0
[v0.1.2]: https://github.com/xmidt-org/hecate/compare/v0.1.1...v0.1.2
[v0.1.1]: https://github.com/xmidt-org/hecate/compare/v0.1.0...v0.1.1
[v0.1.0]: https://github.com/xmidt-org/hecate/compare/v0.1.0...v0.1.0
