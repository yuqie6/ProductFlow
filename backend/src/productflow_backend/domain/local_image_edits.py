"""Database-free authority for image local-edit operations."""

from enum import StrEnum


class LocalImageEditOperation(StrEnum):
    """Operations that can be delegated to a masked image provider.

    Crop is intentionally absent: it is a deterministic application-side
    transformation and must not be represented as a provider edit operation.
    """

    REMOVE = "remove"
    REPLACE_TEXT = "replace_text"
    INPAINT = "inpaint"
