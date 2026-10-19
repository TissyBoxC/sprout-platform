import json
import pathlib
import unittest

import yaml


ROOT = pathlib.Path(__file__).resolve().parents[1]
SCHEMA_PATH = ROOT / "packages/contracts/schemas/ota_device_event.schema.json"
EXAMPLE_PATH = ROOT / "packages/contracts/schemas/ota_device_event.example.json"
OPENAPI_PATH = ROOT / "services/device_platform/internal/contracts/http/openapi.yaml"

EXPECTED_EVENT_TYPES = [
    "started",
    "downloading",
    "downloaded",
    "validated",
    "installing",
    "pending_verify",
    "succeeded",
    "failed",
    "rollback_started",
    "rolled_back",
]

EXPECTED_REQUIRED_FIELDS = {
    "schema_version",
    "event_id",
    "deployment_id",
    "release_id",
    "event_type",
    "sequence",
    "progress_percent",
    "firmware_version",
    "occurred_at",
}

EXPECTED_FIELDS = {
    "schema_version",
    "event_id",
    "deployment_id",
    "release_id",
    "event_type",
    "sequence",
    "progress_percent",
    "bytes_received",
    "bytes_total",
    "error_code",
    "message",
    "detail",
    "occurred_at",
    "firmware_version",
    "artifact_sha256",
}


class OTAEventContractTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.schema = json.loads(SCHEMA_PATH.read_text(encoding="utf-8"))
        cls.example = json.loads(EXAMPLE_PATH.read_text(encoding="utf-8"))
        cls.openapi = yaml.safe_load(OPENAPI_PATH.read_text(encoding="utf-8"))

    def openapi_schema(self, name):
        return self.openapi["components"]["schemas"][name]

    def test_shared_schema_uses_canonical_event_vocabulary(self):
        self.assertEqual(
            self.schema["properties"]["event_type"]["enum"],
            EXPECTED_EVENT_TYPES,
        )

    def test_shared_schema_requires_identity_timestamp_and_sequence(self):
        self.assertEqual(
            set(self.schema["required"]),
            EXPECTED_REQUIRED_FIELDS,
        )
        self.assertEqual(
            set(self.schema["properties"]),
            EXPECTED_FIELDS,
        )
        self.assertEqual(
            self.schema["properties"]["sequence"]["minimum"],
            1,
        )

    def test_openapi_uses_the_same_event_vocabulary(self):
        event_type = self.openapi_schema("OTAEventType")
        self.assertEqual(event_type["enum"], EXPECTED_EVENT_TYPES)

    def test_openapi_device_update_documents_nested_payload(self):
        update = self.openapi_schema("DeviceUpdate")
        self.assertIn("schema_version", update["required"])
        self.assertEqual(
            update["properties"]["schema_version"]["const"],
            "1.0.0",
        )

    def test_openapi_request_accepts_and_requires_shared_fields(self):
        request = self.openapi_schema("OTADeviceEventRequest")
        self.assertFalse(request["additionalProperties"])
        self.assertEqual(
            set(request["required"]),
            EXPECTED_REQUIRED_FIELDS,
        )
        self.assertEqual(
            set(request["properties"]),
            EXPECTED_FIELDS,
        )
        self.assertEqual(request["properties"]["sequence"]["minimum"], 1)

    def test_example_is_accepted_by_the_shared_schema_contract(self):
        self.assertLessEqual(set(self.example), EXPECTED_FIELDS)
        self.assertIn(
            self.example["event_type"],
            EXPECTED_EVENT_TYPES,
        )
        for field in EXPECTED_REQUIRED_FIELDS:
            self.assertIn(field, self.example)


if __name__ == "__main__":
    unittest.main()
