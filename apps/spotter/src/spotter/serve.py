# apps/spotter/src/spotter/serve.py

import asyncio
import base64
import os
from io import BytesIO

import httpx
import torch
from PIL import Image, ImageDraw
from ray import serve
from starlette.requests import Request
from tenacity import AsyncRetrying, stop_after_attempt, wait_exponential
from transformers import (
    AutoImageProcessor,
    AutoModelForObjectDetection,
    BaseImageProcessor,
    PreTrainedModel,
)

from spotter.schemas import (
    DetectionErrorResult,
    DetectionRequest,
    DetectionResponse,
    DetectionResult,
    DetectionSuccessResult,
    ImageResult,
)

# Mapping of COCO labels to amenities
AMENITIES_MAPPING = {
    # Kitchen
    "refrigerator": "refrigerator",
    "oven": "oven",
    "microwave": "microwave",
    "sink": "sink",  # Could be kitchen or bathroom
    "dining table": "dining area",
    "toaster": "toaster",
    "wine glass": "kitchen",
    "cup": "kitchen",
    "fork": "kitchen",
    "knife": "kitchen",
    "spoon": "kitchen",
    "bowl": "kitchen",
    # Living Area
    "tv": "TV",
    "couch": "sofa",
    "chair": "chair",
    # Bedroom
    "bed": "bed",
    # Bathroom
    "toilet": "bathroom",
    "hair drier": "hair dryer",
    # Workspace indicator
    "laptop": "workspace",
    "mouse": "workspace",
    "keyboard": "workspace",
    "car": "parking",
}

device = torch.device("mps" if torch.backends.mps.is_available() else "cpu")


@serve.deployment
class AmenitiesDetector:
    def __init__(self, model: PreTrainedModel, processor: BaseImageProcessor) -> None:
        if not hasattr(processor, "post_process_object_detection"):
            raise ValueError("Image processor must implement post_process_object_detection method")

        self.model = model
        self.processor = processor
        self.client = httpx.AsyncClient()

    async def _fetch_image_bytes(self, url: str) -> bytes:
        response = await self.client.get(url)
        response.raise_for_status()
        return response.content

    async def _process_single_image(self, url: str) -> ImageResult:
        """Processes a single image URL, handling detection, drawing, and encoding."""

        try:
            image_bytes = None
            retries = AsyncRetrying(
                stop=stop_after_attempt(3),
                wait=wait_exponential(multiplier=1, min=4, max=10),
                reraise=True,
            )
            async for attempt in retries:
                with attempt:
                    image_bytes = await self._fetch_image_bytes(url)

            if image_bytes is None:
                raise Exception("Failed to fetch image after retries")

            with Image.open(BytesIO(image_bytes)) as img_raw:
                image = img_raw.convert("RGB")
                inputs = self.processor(images=image, return_tensors="pt").to(device)
                with torch.no_grad():
                    outputs = self.model(**inputs)

                target_sizes = torch.tensor([[image.size[1], image.size[0]]])
                detections_raw: dict[str, torch.Tensor] = (
                    self.processor.post_process_object_detection(  # type: ignore
                        outputs,
                        target_sizes=target_sizes,
                        threshold=0.5,
                    )[0]
                )

                labels = [
                    self.model.config.id2label[int(label.item())]
                    for label in detections_raw["labels"]
                ]

                # [xmin, ymin, xmax, ymax]
                boxes: list[list[float]] = detections_raw["boxes"].tolist()

                draw = ImageDraw.Draw(image)
                image_detections: list[DetectionResult] = []
                drawn_amenities_for_image = set()  # Track amenities for this image only

                for label, box in zip(labels, boxes):
                    if label not in AMENITIES_MAPPING:
                        continue
                    amenity = AMENITIES_MAPPING[label]
                    draw.rectangle(box, outline="red", width=3)
                    draw.text(
                        xy=(box[0] + 5, box[1] + 5),
                        text=amenity,
                        fill="white",
                        stroke_width=1,
                        stroke_fill="black",
                    )
                    image_detections.append(DetectionResult(label=amenity, box=box))
                    drawn_amenities_for_image.add(amenity)

                # Encode the modified image to base64
                buffer = BytesIO()
                image.save(buffer, format="JPEG")
                image_with_boxes_bytes = buffer.getvalue()
                image_b64 = base64.b64encode(image_with_boxes_bytes).decode("utf-8")

                return DetectionSuccessResult(
                    url=url,
                    detections=image_detections,
                    labeled_image_base64=image_b64,
                )

        except httpx.HTTPError as e:
            return DetectionErrorResult(url=url, error=f"HTTP Error: {e}")
        except Exception as e:
            import traceback

            tb_str = traceback.format_exc()
            error_msg = f"Processing Error: {e}\n{tb_str}"
            return DetectionErrorResult(url=url, error=error_msg)

    async def __call__(self, raw_payload: Request) -> DetectionResponse:
        """
        Process a batch of images to detect amenities.

        Parameters
        ----------
        payload : Request
            Request payload containing list of image URLs to process.

        Returns
        -------
        DetectionResponse
            Response containing amenities description and per-image results.
            Includes detected amenities, bounding boxes, and labeled images.

        Raises
        ------
        Exception
            If there is an error processing the request.
        """
        payload = DetectionRequest.model_validate(await raw_payload.json())
        tasks = [self._process_single_image(str(image_url)) for image_url in payload.image_urls]
        gathered_results = await asyncio.gather(*tasks)

        all_amenities_detected: set[str] = set()
        final_results: list[ImageResult] = []
        for result in gathered_results:
            final_results.append(result)
            if isinstance(result, DetectionSuccessResult):
                all_amenities_detected.update(detection.label for detection in result.detections)

        amenities_description = (
            f"The property contains: {', '.join(sorted(list(all_amenities_detected)))}."
            if all_amenities_detected
            else "No relevant amenities detected."
        )

        return DetectionResponse(amenities_description=amenities_description, images=final_results)


model_name = os.environ.get("MODEL_NAME")
if not model_name:
    raise ValueError("MODEL_NAME environment variable not set.")

model = AutoModelForObjectDetection.from_pretrained(model_name).to(device)  # type: ignore
processor = AutoImageProcessor.from_pretrained(model_name)
deployment = AmenitiesDetector.bind(model, processor)
