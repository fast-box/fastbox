
<h1 align="center">FastBox</h1>
<h4 align="center">Version 0.0.1</h4>

Welcome to the official Go implementation of the [FastBox](http://fastbox.link) blockchain!

English | [中文](README_CN.md)

FastBox is a high-performance blockchain project and distributed trust collaboration platform.

New features are still being rapidly developed, therefore the master branch may be unstable. Stable versions can be found in the [releases section](https://github.com/fast-box/fastbox/releases).

## Install from Binaries
You can download a stable compiled version of the fastbox node software from the [release section](https://github.com/fast-box/fastbox/releases).

## Build From Source

### Prerequisites

- [Golang](https://golang.org/doc/install) version 1.12 or later


### Build

Note that the code in the `master` branch may not be stable.

```
$ git clone https://github.com/fast-box/fastbox
$ cd fastbox
$ make
```

After building the source code successfully, you should see the executable programs in `build/bin` dictionary:

- `fbox`: The primary fastbox node application and CLI.
- `promfile`: Used to create genesis.json.

## Run FastBox

The FastBox can run nodes for the TestNet and local PrivateNet. Look up [FastBox user guide](https://github.com/fast-box/fastbox/wiki) for all guides.

## Examples

For further examples, please refer to the [CLI User Guide](https://github.com/fast-box/fastbox).

## Contributions

Contributors to FastBox are very welcome! Before beginning, please take a look at our [contributing guidelines](CONTRIBUTING.md). You can open an issue by [clicking here](https://github.com/fast-box/fastbox/issues/new).

## License

The FastBox source code is available under the [LGPL-3.0](LICENSE) license.
