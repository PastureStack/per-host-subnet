.PHONY: test validate build package

test:
	./scripts/test

validate:
	./scripts/validate

build:
	./scripts/build

package:
	CROSS=1 ./scripts/build
	./scripts/package
