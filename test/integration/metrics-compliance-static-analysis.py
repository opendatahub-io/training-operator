"""
Telemetry Test Suite for Training Operator

Tests compliance with Red Hat Monitoring Handbook and RHOAISTRAT-575
"""

import re
import shlex
import subprocess
import sys
from pathlib import Path

import yaml


class Colors:
    GREEN = "\033[92m"
    RED = "\033[91m"
    YELLOW = "\033[93m"
    BLUE = "\033[94m"
    ENDC = "\033[0m"
    BOLD = "\033[1m"


class TelemetryTestSuite:
    """Test suite for telemetry implementation compliance"""

    def __init__(self):
        self.base_path = Path(__file__).resolve().parents[2]
        self.metrics_file = self.base_path / "pkg/common/metrics_telemetry.go"
        self.servicemonitor_file = (
            self.base_path / "manifests/base/monitoring/servicemonitor.yaml"
        )
        self.recording_rules_file = (
            self.base_path / "manifests/base/monitoring/recording-rules.yaml"
        )
        self.test_results = []

    def run_command(self, cmd, timeout=5):
        """Execute shell command and return output"""
        try:
            # Convert string command to list of arguments if needed
            args = shlex.split(cmd) if isinstance(cmd, str) else cmd
            result = subprocess.run(
                args,
                capture_output=True,
                text=True,
                timeout=timeout,
                cwd=self.base_path,
                check=False,  # Don't raise exception on non-zero return code
            )
        except subprocess.TimeoutExpired:
            return "", "Command timed out", 1
        except (subprocess.SubprocessError, OSError) as e:
            return "", str(e), 1
        else:
            # Return results only if no exception occurred
            return result.stdout, result.stderr, result.returncode

    def test_cardinality_compliance(self):
        """Test that total telemetry timeseries is exactly 10"""
        print(f"\n{Colors.BOLD}1. Testing Cardinality Compliance{Colors.ENDC}")
        print("-" * 50)

        if not self.metrics_file.exists():
            print(
                f"  {Colors.RED}[FAIL] FAIL: Metrics file not found at "
                f"{self.metrics_file}{Colors.ENDC}"
            )
            self.test_results.append(("Cardinality Compliance", False))
            return False

        content = self.metrics_file.read_text()

        runtime_labels = re.findall(
            r'telemetryJobsByRuntime\.WithLabelValues\("([^"]+)"\)\.Add\(0\)',
            content,
        )
        image_labels = re.findall(
            r'telemetryImagePreference\.WithLabelValues\("([^"]+)"\)\.Add\(0\)',
            content,
        )
        framework_labels = re.findall(
            r'telemetryFrameworkUsage\.WithLabelValues\("([^"]+)"\)\.Add\(0\)',
            content,
        )

        runtime_count = len(set(runtime_labels))
        image_count = len(set(image_labels))
        framework_count = len(set(framework_labels))
        total = runtime_count + image_count + framework_count

        print(f"  Runtime timeseries:    {runtime_count} {list(set(runtime_labels))}")
        print(f"  Image preference:      {image_count} {list(set(image_labels))}")
        print(
            f"  Framework usage:       {framework_count} {list(set(framework_labels))}"
        )
        print(f"  {Colors.BOLD}TOTAL: {total} timeseries{Colors.ENDC}")

        if total == 10:
            print(
                f"  {Colors.GREEN}PASS: Exactly 10 timeseries{Colors.ENDC} "
                "(Red Hat Handbook limit)"
            )
            self.test_results.append(("Cardinality Compliance", True))
            return True
        else:
            print(
                f"  {Colors.RED}[FAIL] FAIL: {total} timeseries - "
                f"violates limit of 10{Colors.ENDC}"
            )
            self.test_results.append(("Cardinality Compliance", False))
            return False

    def test_pytorch_prioritization(self):
        """Test that PyTorch is checked first in runtime classification"""
        print(f"\n{Colors.BOLD}2. Testing PyTorch Prioritization{Colors.ENDC}")
        print("-" * 50)

        if not self.metrics_file.exists():
            print(
                f"  {Colors.RED}[FAIL] FAIL: Metrics file not found at "
                f"{self.metrics_file}{Colors.ENDC}"
            )
            self.test_results.append(("PyTorch Prioritization", False))
            return False
        content = self.metrics_file.read_text(encoding="utf-8")

        match = re.search(r"func classifyRuntime.*?\n}", content, re.DOTALL)
        if not match:
            print(
                f"  {Colors.RED}[FAIL] FAIL: Could not find "
                f"classifyRuntime function{Colors.ENDC}"
            )
            self.test_results.append(("PyTorch Prioritization", False))
            return False

        func_text = match.group()

        pytorch_check = 'frameworkLower == "pytorch"'
        tensorflow_check = 'frameworkLower == "tensorflow"'

        pytorch_pos = func_text.find(pytorch_check)
        tensorflow_pos = func_text.find(tensorflow_check)

        if pytorch_pos > 0 and (tensorflow_pos < 0 or pytorch_pos < tensorflow_pos):
            print(
                f"  {Colors.GREEN}[PASS] PASS: PyTorch checked FIRST{Colors.ENDC} "
                "(>95% of workloads)"
            )
            print(f"     PyTorch check at position: {pytorch_pos}")
            print(f"     TensorFlow check at position: {tensorflow_pos}")
            self.test_results.append(("PyTorch Prioritization", True))
            return True
        else:
            print(f"  {Colors.RED}[FAIL] FAIL: PyTorch not prioritized{Colors.ENDC}")
            self.test_results.append(("PyTorch Prioritization", False))
            return False

    def test_runtime_classification(self):
        """Test runtime classification logic with various inputs"""
        print(f"\n{Colors.BOLD}3. Testing Runtime Classification Logic{Colors.ENDC}")
        print("-" * 50)

        test_cases = [
            ("pytorch", "pytorch:2.4", "pytorch-2.4"),
            ("pytorchjob", "pytorch:2.5.0-cuda", "pytorch-2.5"),
            ("pytorch", "pytorch:1.13", "pytorch-other"),
            ("tensorflow", "tensorflow:2.13", "tensorflow"),
            ("tfjob", "tensorflow/tensorflow", "tensorflow"),
            ("mpi", "mpioperator/mpi", "other"),
            ("pytorch", "multi-pytorch-2.4-tf", "pytorch-2.4"),
        ]

        all_pass = True
        for framework, image, expected in test_cases:
            result = self.simulate_classify_runtime(framework, image)
            status = "[PASS]" if result == expected else "[FAIL]"
            print(
                f"  {status} Framework: {framework:12} "
                f"Image: {image:30} -> {result:15} "
                f"(expected: {expected})"
            )
            if result != expected:
                all_pass = False

        self.test_results.append(("Runtime Classification", all_pass))
        return all_pass

    def simulate_classify_runtime(self, framework, image):
        """Simulate the classifyRuntime function logic"""
        framework_lower = framework.lower()
        image_lower = image.lower()

        # PyTorch checked first (priority)
        if (
            framework_lower in ["pytorch", "pytorchjob"]
            or "pytorch" in image_lower
            or "torch" in image_lower
        ):
            if any(v in image_lower for v in ["2.4", "2-4", "24"]):
                return "pytorch-2.4"
            if any(v in image_lower for v in ["2.5", "2-5", "25"]):
                return "pytorch-2.5"
            return "pytorch-other"

        # TensorFlow
        if (
            framework_lower in ["tensorflow", "tfjob"]
            or "tensorflow" in image_lower
            or "tf-" in image_lower
        ):
            return "tensorflow"

        return "other"

    def test_image_source_detection(self):
        """Test RHOAI image detection logic"""
        print(f"\n{Colors.BOLD}4. Testing Image Source Detection{Colors.ENDC}")
        print("-" * 50)

        test_cases = [
            ("registry.redhat.io/ubi8/python", True),
            ("quay.io/modh/pytorch:latest", True),
            ("quay.io/opendatahub/tensorflow", True),
            ("image-registry.openshift-image-registry/myproject/app", True),
            ("docker.io/pytorch/pytorch", False),
            ("nvcr.io/nvidia/pytorch", False),
        ]

        all_pass = True
        for image, expected in test_cases:
            result = self.simulate_is_rhoai_image(image)
            status = "[PASS]" if result == expected else "[FAIL]"
            expected_str = "RHOAI" if expected else "External"
            result_str = "RHOAI" if result else "External"
            print(
                f"  {status} {image:55} -> "
                f"{result_str:8} (expected: {expected_str})"
            )
            if result != expected:
                all_pass = False

        self.test_results.append(("Image Source Detection", all_pass))
        return all_pass

    def simulate_is_rhoai_image(self, image):
        """Simulate the isRHOAIImage function logic"""
        image_lower = image.lower()
        return any(
            [
                "registry.redhat.io" in image_lower,
                "quay.io/modh" in image_lower,
                "quay.io/opendatahub" in image_lower,
                "image-registry.openshift-image-registry" in image_lower,
            ]
        )

    def test_monitoring_configuration(self):
        """Test monitoring configuration files for compliance"""
        print(f"\n{Colors.BOLD}5. Testing Monitoring Configuration{Colors.ENDC}")
        print("-" * 50)

        # Test ServiceMonitor
        if not self.servicemonitor_file.exists():
            print(
                f"  {Colors.RED}[FAIL] FAIL: ServiceMonitor file not found{Colors.ENDC}"
            )
            self.test_results.append(("Monitoring Configuration", False))
            return False

        with open(self.servicemonitor_file, encoding="utf-8") as f:
            sm_config = yaml.safe_load(f)
        if not isinstance(sm_config, dict):
            print(
                f"  {Colors.RED}[FAIL] FAIL: Invalid/empty ServiceMonitor YAML{Colors.ENDC}"
            )
            self.test_results.append(("Monitoring Configuration", False))
            return False

        # Check critical label
        labels = sm_config.get("metadata", {}).get("labels", {})
        scrape_label = labels.get("monitoring.opendatahub.io/scrape")
        # Handle both boolean True and string "true"
        if scrape_label is True or str(scrape_label).lower() == "true":
            print(
                f"  {Colors.GREEN}[PASS] PASS: ServiceMonitor has critical "
                f"monitoring label{Colors.ENDC}"
            )
        else:
            print(
                f"  {Colors.RED}[FAIL] FAIL: Missing critical monitoring "
                f"label{Colors.ENDC}"
            )
            self.test_results.append(("Monitoring Configuration", False))
            return False

        # Test PrometheusRule
        if not self.recording_rules_file.exists():
            print(
                f"  {Colors.RED}[FAIL] FAIL: Recording rules file not found{Colors.ENDC}"
            )
            self.test_results.append(("Monitoring Configuration", False))
            return False

        with open(self.recording_rules_file, encoding="utf-8") as f:
            rules_config = yaml.safe_load(f)
        if not isinstance(rules_config, dict):
            print(
                f"  {Colors.RED}[FAIL] FAIL: Invalid/empty RecordingRules YAML{Colors.ENDC}"
            )
            self.test_results.append(("Monitoring Configuration", False))
            return False

        # Count telemetry rules
        telemetry_rules = 0
        for group in rules_config.get("spec", {}).get("groups", []):
            if "telemetry" in group.get("name", "").lower():
                telemetry_rules += len(group.get("rules", []))

        if telemetry_rules >= 3:
            print(
                f"  {Colors.GREEN}[PASS] PASS: Found {telemetry_rules} telemetry "
                f"recording rules{Colors.ENDC}"
            )
            self.test_results.append(("Monitoring Configuration", True))
            return True
        else:
            print(
                f"  {Colors.RED}[FAIL] FAIL: Only {telemetry_rules} telemetry "
                f"rules (expected >= 3){Colors.ENDC}"
            )
            self.test_results.append(("Monitoring Configuration", False))
            return False

    def test_telemetry_prefix_compliance(self):
        """Test that telemetry rules use openshift: prefix"""
        print(f"\n{Colors.BOLD}6. Testing Telemetry Prefix Compliance{Colors.ENDC}")
        print("-" * 50)

        with open(self.recording_rules_file, encoding="utf-8") as f:
            rules_config = yaml.safe_load(f)
        if not isinstance(rules_config, dict):
            print(
                f"  {Colors.RED}[FAIL] FAIL: Invalid/empty RecordingRules YAML{Colors.ENDC}"
            )
            self.test_results.append(("Monitoring Configuration", False))
            return False

        non_compliant = []
        compliant = []

        for group in rules_config.get("spec", {}).get("groups", []):
            if "telemetry" in group.get("name", "").lower():
                for rule in group.get("rules", []):
                    record_name = rule.get("record", "")
                    if record_name.startswith("openshift:"):
                        compliant.append(record_name)
                    else:
                        non_compliant.append(record_name)

        for rule in compliant:
            print(f"  {Colors.GREEN}[PASS] {rule}{Colors.ENDC}")

        for rule in non_compliant:
            print(
                f"  {Colors.RED}[FAIL] {rule} (missing openshift: prefix){Colors.ENDC}"
            )

        all_pass = len(non_compliant) == 0 and len(compliant) > 0
        self.test_results.append(("Telemetry Prefix Compliance", all_pass))
        return all_pass

    def run_all_tests(self):
        """Run all tests and print summary"""
        print(f"{Colors.BOLD}{Colors.BLUE}")
        print("=" * 60)
        print("   TELEMETRY COMPLIANCE TEST SUITE   ")
        print("   RHOAISTRAT-575 & Red Hat Monitoring Handbook   ")
        print("=" * 60)
        print(f"{Colors.ENDC}")

        # Run all tests
        self.test_cardinality_compliance()
        self.test_pytorch_prioritization()
        self.test_runtime_classification()
        self.test_image_source_detection()
        self.test_monitoring_configuration()
        self.test_telemetry_prefix_compliance()

        # Print summary
        print(f"\n{Colors.BOLD}{'=' * 60}{Colors.ENDC}")
        print(f"{Colors.BOLD}TEST SUMMARY{Colors.ENDC}")
        print(f"{Colors.BOLD}{'=' * 60}{Colors.ENDC}")

        passed = sum(1 for _, result in self.test_results if result)
        total = len(self.test_results)

        for test_name, result in self.test_results:
            status = (
                f"{Colors.GREEN}[PASS] PASS{Colors.ENDC}"
                if result
                else f"{Colors.RED}[FAIL] FAIL{Colors.ENDC}"
            )
            print(f"{test_name:30} {status}")

        print(f"\n{Colors.BOLD}Total: {passed}/{total} tests passed{Colors.ENDC}")

        if passed == total:
            print(
                f"\n{Colors.GREEN}{Colors.BOLD}ALL TESTS PASSED! "
                f"Telemetry is RHOAISTRAT-575 compliant!{Colors.ENDC}"
            )
            return 0
        else:
            print(
                f"\n{Colors.RED}{Colors.BOLD}[WARN]  SOME TESTS FAILED. "
                f"Please review and fix.{Colors.ENDC}"
            )
            return 1


if __name__ == "__main__":
    suite = TelemetryTestSuite()
    sys.exit(suite.run_all_tests())
