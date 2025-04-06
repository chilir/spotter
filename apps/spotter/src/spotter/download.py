# apps/spotter/src/spotter/download.py

import logging
import os

from transformers import AutoImageProcessor, AutoModelForObjectDetection

logging.basicConfig(level=logging.INFO, format="[%(asctime)s] %(levelname)s: %(message)s")
_logger = logging.getLogger(__name__)


def download() -> None:
    """Pre-download model and processor based on environment variable"""
    model_name = os.environ.get("MODEL_NAME")

    if not model_name:
        msg = "MODEL_NAME environment variable not set."
        raise ValueError(msg)

    _logger.info(f"--- Preloading: {model_name} ---")

    _logger.info("Downloading image processor...")
    AutoImageProcessor.from_pretrained(model_name)
    _logger.info("Image processor download complete.")

    _logger.info("Downloading model...")
    AutoModelForObjectDetection.from_pretrained(model_name)
    _logger.info("Model download complete.")

    _logger.info(f"--- Preloading complete for: {model_name} ---")
