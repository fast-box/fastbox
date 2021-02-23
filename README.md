
<h1 align="center">Sphinx</h1>
<h4 align="center">Version 0.0.1</h4>

Welcome to the official Go implementation of the [Sphinx](https://hpb.io) blockchain!

Sphinx is a high-performance blockchain project and distributed trust collaboration platform.

New features are still being rapidly developed, therefore the master branch may be unstable. Stable versions can be found in the [releases section](https://github.com/hpb-project/sphinx/releases).

## Install from Binaries
You can download a stable compiled version of the sphinx node software from the [release section](https://github.com/hpb-project/sphinx/releases).

## Build From Source

### Prerequisites

- [Golang](https://golang.org/doc/install) version 1.12 or later


### Build

Note that the code in the `master` branch may not be stable.

```
$ git clone https://github.com/hpb-project/sphinx
$ cd sphinx
$ make
```

After building the source code successfully, you should see the executable programs in `build/bin` dictionary:

- `shx`: The primary sphinx node application and CLI.

## Run Sphinx

The Sphinx can run nodes for the TestNet and local PrivateNet. Look up [Sphinx user guide](https://github.com/hpb-project/sphinx/wiki) for all guides.

## Examples

For further examples, please refer to the [CLI User Guide](https://github.com/hpb-project/sphinx).

## Contributions

Contributors to Sphinx are very welcome! Before beginning, please take a look at our [contributing guidelines](CONTRIBUTING.md). You can open an issue by [clicking here](https://github.com/hpb-project/sphinx/issues/new).

## License

The Ontology source code is available under the [LGPL-3.0](LICENSE) license.