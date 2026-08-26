"""图片局部编辑操作的无数据库权威定义。"""

from enum import StrEnum


class LocalImageEditOperation(StrEnum):
    """可委托给蒙版图片 provider 的操作。

    Crop 不在此枚举：它是应用侧的确定性变换，不得表示为 provider 编辑操作。
    """

    REMOVE = "remove"
    REPLACE_TEXT = "replace_text"
    INPAINT = "inpaint"
