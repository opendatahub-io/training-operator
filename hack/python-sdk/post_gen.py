#!/usr/bin/env python3

# Copyright 2021 The Kubeflow Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""
This script is used for updating generated SDK files.
"""

import fileinput
import os
import re

__replacements = [
    ("import kubeflow.training", "from kubeflow.training.models import *"),
    ("kubeflow.training.models.v1\/.*.v1.", "V1"),
    ("kubeflow.training.models.kubeflow/org/v1/", "kubeflow_org_v1_"),
    ("\.kubeflow.org.v1\.", ".KubeflowOrgV1"),
]

sdk_dir = os.path.abspath(os.path.join(__file__, "../../..", "sdk/python"))


def main():
    fix_test_files()
    add_imports()
    fix_api_client_dict_deserialize()
    sync_package_version()


def fix_test_files() -> None:
    """
    Fix invalid model imports in generated model tests
    """
    test_folder_dir = os.path.join(sdk_dir, "test")
    test_files = os.listdir(test_folder_dir)
    for test_file in test_files:
        print(f"Processing file {test_file}")
        if test_file.endswith(".py"):
            with fileinput.FileInput(
                os.path.join(test_folder_dir, test_file), inplace=True
            ) as file:
                for line in file:
                    print(_apply_regex(line), end="")


def add_imports() -> None:
    with open(os.path.join(sdk_dir, "kubeflow/training/__init__.py"), "a") as f:
        f.write("from kubeflow.training.api.training_client import TrainingClient\n")
        f.write("from kubeflow.training.constants import constants\n")
    with open(os.path.join(sdk_dir, "kubeflow/__init__.py"), "a") as f:
        f.write("__path__ = __import__('pkgutil').extend_path(__path__, __name__)\n")

    # Add Kubernetes models to proper deserialization of Training models.
    with open(os.path.join(sdk_dir, "kubeflow/training/models/__init__.py"), "r") as f:
        new_lines = []
        for line in f.readlines():
            new_lines.append(line)
            if line.startswith("from __future__ import absolute_import"):
                new_lines.append("\n")
                new_lines.append("# Import Kubernetes models.\n")
                new_lines.append("from kubernetes.client import *\n")
    with open(os.path.join(sdk_dir, "kubeflow/training/models/__init__.py"), "w") as f:
        f.writelines(new_lines)


def _apply_regex(input_str: str) -> str:
    for pattern, replacement in __replacements:
        input_str = re.sub(pattern, replacement, input_str)
    return input_str


def fix_api_client_dict_deserialize() -> None:
    """
    Re-apply midstream fix for kubernetes client >= 36 dict[K, V] bracket syntax.
    OpenAPI generator only emits dict(...) parenthesis form.
    """
    api_client_path = os.path.join(sdk_dir, "kubeflow/training/api_client.py")
    with open(api_client_path, "r") as f:
        content = f.read()

    old_block = (
        "            if klass.startswith('dict('):\n"
        "                sub_kls = re.match(r'dict\\(([^,]*), (.*)\\)', klass).group(2)\n"
        "                return {k: self.__deserialize(v, sub_kls)\n"
        "                        for k, v in six.iteritems(data)}"
    )
    legacy_block = (
        "            if klass.startswith('dict(') or klass.startswith('dict['):\n"
        "                m = re.match(r'dict[\\(\\[]\\s*([^,]*?)\\s*,\\s*(.*?)\\s*[\\)\\]]$', klass)\n"
        "                if m is None:\n"
        "                    raise ApiValueError(\n"
        '                        "Failed to parse dict type: {}".format(klass))\n'
        "                sub_kls = m.group(2)\n"
        "                return {k: self.__deserialize(v, sub_kls)\n"
        "                        for k, v in six.iteritems(data)}"
    )
    patched_block = (
        "            if klass.startswith('dict(') or klass.startswith('dict['):\n"
        "                if klass.startswith('dict('):\n"
        "                    m = re.match(r'dict\\(\\s*([^,]*?)\\s*,\\s*(.*?)\\s*\\)$', klass)\n"
        "                else:\n"
        "                    m = re.match(r'dict\\[\\s*([^,]*?)\\s*,\\s*(.*?)\\s*\\]$', klass)\n"
        "                if m is None:\n"
        "                    raise ApiValueError(\n"
        '                        "Failed to parse dict type: {}".format(klass))\n'
        "                sub_kls = m.group(2)\n"
        "                return {k: self.__deserialize(v, sub_kls)\n"
        "                        for k, v in six.iteritems(data)}"
    )

    if patched_block in content:
        return

    if old_block in content:
        content = content.replace(old_block, patched_block)
    elif legacy_block in content:
        content = content.replace(legacy_block, patched_block)
    else:
        raise RuntimeError(
            "api_client.py dict deserialization block has unexpected content; "
            "update fix_api_client_dict_deserialize()"
        )

    with open(api_client_path, "w") as f:
        f.write(content)


def sync_package_version() -> None:
    """Keep __init__.py version aligned with setup.py after regeneration."""
    setup_path = os.path.join(sdk_dir, "setup.py")
    init_path = os.path.join(sdk_dir, "kubeflow/training/__init__.py")

    with open(setup_path, "r") as f:
        setup_content = f.read()

    version_match = re.search(r'version="([^"]+)"', setup_content)
    if version_match is None:
        raise RuntimeError("could not read version from setup.py")

    version = version_match.group(1)

    with open(init_path, "r") as f:
        init_content = f.read()

    version_pattern = re.compile(r'__version__\s*=\s*"([^"]*)"')
    version_match_init = version_pattern.search(init_content)
    if version_match_init is None:
        raise RuntimeError("could not find __version__ in kubeflow/training/__init__.py")

    if version_match_init.group(1) == version:
        return

    updated_init = version_pattern.sub(f'__version__ = "{version}"', init_content, count=1)

    with open(init_path, "w") as f:
        f.write(updated_init)


if __name__ == "__main__":
    main()
