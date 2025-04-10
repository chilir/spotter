# apps/spotter/tests/spotter/test_serve.py

from io import BytesIO
from pathlib import Path

# Note: These global mocks are still unittest.mock.MagicMock.
# Ideally, they should be created within the fixture using mocker for isolation.
# Leaving as is for now to focus on the primary refactoring task.
from unittest.mock import MagicMock  # Keep this temporarily for global mocks

import httpx
import pytest
import torch
from PIL import Image
from pytest_mock import MockerFixture
from transformers import AutoImageProcessor, AutoModelForObjectDetection

from spotter.schemas import (
    DetectionErrorResult,
    DetectionSuccessResult,
)
from spotter.serve import AmenitiesDetector

mock_model = MagicMock()
mock_model.config.id2label = {0: "tv", 1: "couch", 2: "remote"}

mock_processor = MagicMock()
mock_processor.post_process_object_detection = MagicMock()


# Use the .func_or_class attribute to get the underlying class
DetectorClass = AmenitiesDetector.func_or_class


@pytest.fixture
def detector(mocker: MockerFixture) -> DetectorClass:
    """Fixture to create an instance of AmenitiesDetector with mocked dependencies."""
    instance = DetectorClass(model=mock_model, processor=mock_processor)
    instance.client = mocker.AsyncMock(spec=httpx.AsyncClient)
    return instance


@pytest.mark.asyncio
async def test_fetch_image_bytes_success(detector: DetectorClass, mocker: MockerFixture) -> None:
    """Test _fetch_image_bytes successfully retrieves image bytes."""
    mock_url = "http://example.com/image.jpg"
    mock_image_bytes = b"fake_image_data"

    mock_response = mocker.AsyncMock(spec=httpx.Response)
    mock_response.content = mock_image_bytes
    mock_response.raise_for_status = mocker.MagicMock()
    mock_get = mocker.patch.object(detector.client, "get", return_value=mock_response)

    result = await detector._fetch_image_bytes(mock_url)

    mock_get.assert_awaited_once_with(mock_url)
    mock_response.raise_for_status.assert_called_once()
    assert result == mock_image_bytes


@pytest.mark.asyncio
async def test_fetch_image_bytes_http_error(detector: DetectorClass, mocker: MockerFixture) -> None:
    """Test _fetch_image_bytes raises httpx.HTTPStatusError on bad response."""
    mock_url = "http://example.com/notfound.jpg"
    mock_error = httpx.HTTPStatusError(
        "Not Found", request=mocker.MagicMock(), response=mocker.MagicMock()
    )

    mock_response = mocker.AsyncMock(spec=httpx.Response)
    mock_response.raise_for_status = mocker.MagicMock(side_effect=mock_error)
    mock_get = mocker.patch.object(detector.client, "get", return_value=mock_response)

    with pytest.raises(httpx.HTTPStatusError):
        await detector._fetch_image_bytes(mock_url)

    mock_get.assert_awaited_once_with(mock_url)
    mock_response.raise_for_status.assert_called_once()


@pytest.mark.asyncio
async def test_process_single_image_success(detector: DetectorClass, mocker: MockerFixture) -> None:
    """Test _process_single_image successfully processes an image."""
    mock_url = "http://example.com/image.jpg"
    mock_image_bytes = b"fake_image_data"
    mock_base64_string = "ZmFrZV9pbWFnZV9kYXRh"  # "fake_image_data" base64 encoded

    mock_image_open = mocker.patch("spotter.serve.Image.open")
    mock_image_draw = mocker.patch("spotter.serve.ImageDraw.Draw")
    mock_b64encode = mocker.patch("spotter.serve.base64.b64encode")

    mock_fetch = mocker.patch.object(detector, "_fetch_image_bytes", return_value=mock_image_bytes)

    mock_img = mocker.MagicMock(spec=Image.Image)
    mock_img.size = (100, 80)
    mock_img.convert.return_value = mock_img
    mock_img.save = mocker.MagicMock()
    mock_image_open.return_value.__enter__.return_value = mock_img

    mock_inputs = {"pixel_values": torch.randn(1, 3, 80, 100)}
    detector.processor.return_value.to.return_value = mock_inputs

    mock_outputs = mocker.MagicMock()
    detector.model.return_value = mock_outputs

    mock_detections_raw = {
        "scores": torch.tensor([0.9, 0.8]),
        "labels": torch.tensor([0, 1]),  # Corresponds to "tv", "couch"
        "boxes": torch.tensor([[10, 10, 50, 50], [60, 20, 90, 70]]),
    }

    detector.processor.post_process_object_detection.return_value = [mock_detections_raw]

    mock_draw_instance = mocker.MagicMock()
    mock_image_draw.return_value = mock_draw_instance

    mock_b64encode.return_value.decode.return_value = mock_base64_string

    result = await detector._process_single_image(mock_url)

    mock_fetch.assert_awaited_once_with(mock_url)
    mock_image_open.assert_called_once()
    assert isinstance(mock_image_open.call_args[0][0], BytesIO)
    assert mock_image_open.call_args[0][0].read() == mock_image_bytes
    mock_img.convert.assert_called_once_with("RGB")
    detector.processor.assert_called_once()
    detector.model.assert_called_once_with(**mock_inputs)
    detector.processor.post_process_object_detection.assert_called_once()

    mock_image_draw.assert_called_once_with(mock_img)
    assert mock_draw_instance.rectangle.call_count == 2
    assert mock_draw_instance.text.call_count == 2

    text_call_args = mock_draw_instance.text.call_args_list[0]
    assert text_call_args[1]["text"] == "TV"
    assert text_call_args[1]["xy"] == (15, 15)

    mock_img.save.assert_called_once()
    assert isinstance(mock_img.save.call_args[0][0], BytesIO)
    mock_b64encode.assert_called_once_with(mock_img.save.call_args[0][0].getvalue())

    assert isinstance(result, DetectionSuccessResult)
    assert result.url == mock_url
    assert result.labeled_image_base64 == mock_base64_string
    assert len(result.detections) == 2
    assert result.detections[0].label == "TV"
    assert result.detections[0].box == [10.0, 10.0, 50.0, 50.0]
    assert result.detections[1].label == "sofa"
    assert result.detections[1].box == [60.0, 20.0, 90.0, 70.0]


@pytest.mark.asyncio
async def test_process_single_image_fetch_error(
    detector: DetectorClass, mocker: MockerFixture
) -> None:
    """Test _process_single_image returns DetectionErrorResult on fetch failure."""
    mock_url = "http://example.com/image.jpg"
    mock_error = httpx.HTTPStatusError(
        "Not Found",
        request=mocker.MagicMock(),
        response=mocker.MagicMock(),
    )

    mock_fetch = mocker.patch.object(detector, "_fetch_image_bytes", side_effect=mock_error)

    result = await detector._process_single_image(mock_url)

    assert isinstance(result, DetectionErrorResult)
    assert result.url == mock_url
    assert "HTTP Error: Not Found" in result.error
    mock_fetch.assert_awaited()


@pytest.mark.asyncio
async def test_process_single_image_processing_error(
    detector: DetectorClass, mocker: MockerFixture
) -> None:
    """Test _process_single_image returns DetectionErrorResult on image processing failure."""
    mock_url = "http://example.com/image.jpg"
    mock_image_bytes = b"fake_image_data"

    mock_image_open = mocker.patch("spotter.serve.Image.open", side_effect=Exception("PIL Error"))
    mock_fetch = mocker.patch.object(detector, "_fetch_image_bytes", return_value=mock_image_bytes)

    result = await detector._process_single_image(mock_url)

    assert isinstance(result, DetectionErrorResult)
    assert result.url == mock_url
    assert "Processing Error: PIL Error" in result.error
    mock_fetch.assert_awaited_once_with(mock_url)
    mock_image_open.assert_called_once()


@pytest.mark.asyncio
async def test_process_single_image_no_relevant_detections(
    detector: DetectorClass, mocker: MockerFixture
) -> None:
    """Test _process_single_image when detections have no relevant amenities."""
    mock_url = "http://example.com/image.jpg"
    mock_image_bytes = b"fake_image_data"
    mock_base64_string = "ZmFrZV9pbWFnZV9kYXRh"

    mock_image_open = mocker.patch("spotter.serve.Image.open")
    mock_image_draw = mocker.patch("spotter.serve.ImageDraw.Draw")
    mock_b64encode = mocker.patch("spotter.serve.base64.b64encode")

    mocker.patch.object(detector, "_fetch_image_bytes", return_value=mock_image_bytes)

    mock_img = mocker.MagicMock(spec=Image.Image)
    mock_img.size = (100, 80)
    mock_img.convert.return_value = mock_img
    mock_img.save = mocker.MagicMock()
    mock_image_open.return_value.__enter__.return_value = mock_img

    mock_inputs = {"pixel_values": torch.randn(1, 3, 80, 100)}
    detector.processor.return_value.to.return_value = mock_inputs

    mock_outputs = mocker.MagicMock()
    detector.model.return_value = mock_outputs

    mock_detections_raw = {
        "scores": torch.tensor([0.9]),
        "labels": torch.tensor([2]),  # Corresponds to "remote"
        "boxes": torch.tensor([[10, 10, 50, 50]]),
    }
    detector.processor.post_process_object_detection.return_value = [mock_detections_raw]

    mock_draw_instance = mocker.MagicMock()
    mock_image_draw.return_value = mock_draw_instance
    mock_b64encode.return_value.decode.return_value = mock_base64_string

    result = await detector._process_single_image(mock_url)

    assert isinstance(result, DetectionSuccessResult)
    assert result.url == mock_url
    assert result.labeled_image_base64 == mock_base64_string
    assert len(result.detections) == 0
    mock_draw_instance.rectangle.assert_not_called()
    mock_draw_instance.text.assert_not_called()
    mock_img.save.assert_called_once()
    mock_b64encode.assert_called_once()


# --- Integration Tests --- #


MODEL_NAME = "PekingU/rtdetr_v2_r101vd"


@pytest.fixture(scope="session")
def real_detector() -> DetectorClass:
    """Fixture to create a detector with the real model and processor."""

    device = torch.device("mps" if torch.backends.mps.is_available() else "cpu")
    print(f"\nUsing device: {device} for integration test")

    model = AutoModelForObjectDetection.from_pretrained(MODEL_NAME).to(device)  # type: ignore
    processor = AutoImageProcessor.from_pretrained(MODEL_NAME)

    detector_instance = DetectorClass(model=model, processor=processor)
    return detector_instance


@pytest.mark.asyncio
@pytest.mark.integration
@pytest.mark.slow
async def test_real_inference_local_image(
    real_detector: DetectorClass, mocker: MockerFixture
) -> None:
    """Test real inference using a locally stored image."""
    test_dir = Path(__file__).parent
    test_image_path = test_dir / "test_data/test_pic.jpg"
    local_image_id = "local_test_image.jpg"  # Dummy ID for processing

    if not test_image_path.is_file():
        pytest.skip(f"Test image not found at {test_image_path}")

    # Read local image bytes
    with test_image_path.open("rb") as f:
        image_bytes = f.read()

    # Mock the _fetch_image_bytes method to return local bytes
    mock_fetch = mocker.patch.object(real_detector, "_fetch_image_bytes", return_value=image_bytes)

    result = await real_detector._process_single_image(local_image_id)

    assert isinstance(result, DetectionSuccessResult)
    assert result.url == local_image_id
    assert isinstance(result.labeled_image_base64, str)
    assert len(result.labeled_image_base64) > 500  # Check for reasonable length

    assert len(result.detections) > 0
    detected_amenities = {d.label for d in result.detections}
    expected_detected_set = {"kitchen", "oven", "chair"}
    assert detected_amenities == expected_detected_set

    expected_boxes = {
        "kitchen": pytest.approx([305.8487, 331.8141, 352.8352, 360.6238], abs=1.0),
        "oven": pytest.approx([265.7876, 368.4354, 362.2969, 505.2321], abs=1.0),
        "chair": pytest.approx([587.5251, 441.0653, 796.3880, 714.2424], abs=1.0),
    }

    matched_expected_boxes = set()

    for detection in result.detections:
        assert detection.label in expected_detected_set
        assert isinstance(detection.box, list)
        assert len(detection.box) == 4
        assert all(isinstance(coord, (float, int)) for coord in detection.box)

        # Check box validity
        xmin, ymin, xmax, ymax = detection.box
        assert xmin >= 0
        assert ymin >= 0
        assert xmax > xmin
        assert ymax > ymin

        # Check approximate values
        print(f"Checking box for {detection.label}: {detection.box}")
        if detection.label in expected_boxes:
            if detection.box == expected_boxes[detection.label]:
                print(f"Found approximate match for {detection.label}")
                matched_expected_boxes.add(detection.label)
        else:
            print(f"Warning: No approximate box defined for label '{detection.label}'")

    assert matched_expected_boxes == set(expected_boxes.keys())
    mock_fetch.assert_awaited_once_with(local_image_id)
