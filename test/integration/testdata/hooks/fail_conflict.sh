#!/bin/sh
# Always fails with a structured connector error, for try/except tests.
cat > /dev/null
echo '{"message": "resource already exists", "code": 409, "tags": ["HttpError"]}' >&2
exit 1
