from pydantic import BaseModel, HttpUrl


class DetectionRequest(BaseModel):
    image_urls: list[HttpUrl]


class DetectionResult(BaseModel):
    label: str
    # A bounding box is typically [xmin, ymin, xmax, ymax]
    box: list[float]


class DetectionSuccessResult(BaseModel):
    url: str
    detections: list[DetectionResult]
    labeled_image_base64: str


class DetectionErrorResult(BaseModel):
    url: str
    error: str


ImageResult = DetectionSuccessResult | DetectionErrorResult


class DetectionResponse(BaseModel):
    amenities_description: str
    images: list[ImageResult]
