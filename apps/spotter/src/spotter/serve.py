# python/src/serve.py

import asyncio
import base64
from io import BytesIO

import httpx
import torch
from PIL import Image, ImageDraw
from tenacity import AsyncRetrying, stop_after_attempt, wait_exponential
from transformers import (
    RTDetrImageProcessor,
    RTDetrV2ForObjectDetection,
    RTDetrV2PreTrainedModel,
)

from spotter.schemas import (
    DetectionErrorResult,
    DetectionRequest,
    DetectionResponse,
    DetectionResult,
    DetectionSuccessResult,
    ImageResult,
)

DEVICE = torch.device("mps" if torch.backends.mps.is_available() else "cpu")


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


# @serve.deployment
class AmenitiesDetector:
    def __init__(self, model: RTDetrV2PreTrainedModel, processor: RTDetrImageProcessor) -> None:
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
            retries = AsyncRetrying(
                stop=stop_after_attempt(3),
                wait=wait_exponential(multiplier=1, min=4, max=10),
                reraise=True,
            )

            image_bytes = None
            async for attempt in retries:
                with attempt:
                    image_bytes = await self._fetch_image_bytes(url)

            if image_bytes is None:
                raise Exception("Failed to fetch image after retries")

            with Image.open(BytesIO(image_bytes)) as img_raw:
                image = img_raw.convert("RGB")

                inputs = self.processor(images=image, return_tensors="pt").to(DEVICE)
                with torch.no_grad():
                    outputs = self.model(**inputs)

                target_sizes = torch.tensor([image.size[::-1]], device=DEVICE, dtype=torch.int64)
                detections_raw: dict[str, torch.Tensor] = (
                    self.processor.post_process_object_detection(
                        outputs,
                        target_sizes=target_sizes,  # type: ignore
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

    async def __call__(self, payload: DetectionRequest) -> DetectionResponse:
        """
        Process a batch of images to detect amenities.

        Parameters
        ----------
        payload : AmenitiesRequest
            Request payload containing list of image URLs to process.

        Returns
        -------
        AmenitiesResponse
            Response containing amenities description and per-image results.
            Includes detected amenities, bounding boxes, and labeled images.

        Raises
        ------
        Exception
            If there is an error processing the request.
        """

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


def main():
    # --- Ray Serve Deployment --- (Commented out for local testing)
    # ray.init(address="auto")
    # model = DetrForObjectDetection.from_pretrained("facebook/detr-resnet-101").to(DEVICE)
    # processor = DetrImageProcessor.from_pretrained("facebook/detr-resnet-101")
    # detector_app = AmenitiesDetector.bind(model, processor)
    # serve.run(detector_app)
    # ---------------------------

    # --- Local Testing --- (Active)
    TEST_URL = "https://hospitable.com/wp-content/uploads/2022/01/Airbnb-pictures.jpg"  # Example: Cat and Remote

    print(f"Using device: {DEVICE}")
    print("Loading model and processor...")
    model = RTDetrV2ForObjectDetection.from_pretrained("PekingU/rtdetr_v2_r101vd").to(DEVICE)  # type: ignore
    processor = RTDetrImageProcessor.from_pretrained("PekingU/rtdetr_v2_r101vd")

    print("Initializing detector for local test...")
    # Instantiate directly for local testing
    detector = AmenitiesDetector(model, processor)

    print(f"Running detection for URL: {TEST_URL}")
    # Create the Pydantic request model instance directly
    # Pydantic will validate the URL format here too
    try:
        # The linter might complain here, but Pydantic v2 automatically
        # converts the string URL to HttpUrl upon validation.
        request_payload = DetectionRequest(image_urls=[TEST_URL])  # type: ignore
    except Exception as e:
        print(f"Error creating request payload: {e}")
        return

    # Run the async __call__ method, passing the Pydantic model
    result = asyncio.run(detector(request_payload))

    print("\n--- Detection Result ---")
    print(result)
    print("------------------------")
    # --------------------
