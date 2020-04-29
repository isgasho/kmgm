# kmgm
**:closed_lock_with_key::link: Generate certs for your cluster, easy way**

[![Build Status][gh-actions-badge]][gh-actions]
[![go report][go-report-badge]][go-report]

kmgm is a [certificate authority](https://en.wikipedia.org/wiki/Certificate_authority) with focus on its ease of use. Setup certificates and deploy to your cluster in minutes!

## Installation

Linux:

```sh
go get -v -u github.com/IPA-CyberLab/kmgm/cmd/... 
```

Mac OSX, Windows: TBD

## Quick start

Setup a new CA:
```sh
kmgm setup
```

Issue a new certificate:
```sh
kmgm issue
```

## License

kmgm is licensed under Apache license version 2.0. See [LICENSE](https://github.com/IPA-CyberLab/kmgm/blob/master/LICENSE) for more information.

<!-- Markdown link & img dfn's -->
[go-report-badge]: https://goreportcard.com/badge/github.com/IPA-CyberLab/kmgm
[go-report]: https://goreportcard.com/report/github.com/IPA-CyberLab/kmgm
[gh-actions-badge]: https://github.com/IPA-CyberLab/kmgm/workflows/Test%20and%20Release/badge.svg
[gh-actions]: https://github.com/IPA-CyberLab/kmgm/actions