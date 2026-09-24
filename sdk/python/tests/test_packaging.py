import sys
from pathlib import Path

if sys.version_info >= (3, 11):
    import tomllib
else:
    import tomli as tomllib


PYPROJECT = Path(__file__).parents[1] / "pyproject.toml"


def test_python_sdk_metadata_requires_patched_pytest():
    project = tomllib.loads(PYPROJECT.read_text())["project"]

    assert project["requires-python"] == ">=3.10"
    assert project["optional-dependencies"]["dev"][0] == "pytest>=9.0.3"
    assert "Programming Language :: Python :: 3.9" not in project["classifiers"]
