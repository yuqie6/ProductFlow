import { DEFAULT_LOCALE, type Locale } from "./i18n";
import type { CanvasTemplateSummary, WorkflowNodeType } from "./types";

interface BuiltInCanvasTemplateText {
  title: string;
  description: string;
  scenarioTitle: string;
  scenarioDescription: string;
  nodes: Record<string, string>;
  outputSlots: Record<string, string>;
  referenceInputHints?: Record<string, string>;
}

const DEFAULT_EXTERNAL_CONNECTION_LABELS: Partial<Record<Locale, Record<string, string>>> = {
  "en-US": { "自动接商品": "Auto-connect product" },
  "ja-JP": { "自动接商品": "商品を自動接続" },
  "vi-VN": { "自动接商品": "Tự động nối sản phẩm" },
};

const BUILT_IN_TEMPLATE_TEXT_EN: Record<string, BuiltInCanvasTemplateText> = {
  "ecommerce-main-image-v1": {
    title: "E-commerce main image",
    description: "Generate a product hero image with a clear subject, benefits, and composition.",
    scenarioTitle: "Main image",
    scenarioDescription: "For product listings and the first screen of the detail page.",
    nodes: {
      product: "Product info",
      copy: "Main-image benefits",
      image: "Generate main image",
      output: "Main image output",
      refine: "Refine main image",
      refined_output: "Refined output",
    },
    outputSlots: {
      output: "Main image output",
      refined_output: "Refined output",
    },
  },
  "ecommerce-taobao-main-image-v1": {
    title: "Taobao main image",
    description: "Generate a 1:1 product main image for Taobao search, recommendations, and detail entry.",
    scenarioTitle: "Taobao main image",
    scenarioDescription: "For Taobao listing traffic and the detail first screen, with a clear subject and benefits.",
    nodes: {
      product: "Product info",
      angle: "Search benefits",
      main: "Main image version",
      main_output: "Taobao main image",
      clean: "Clean version",
      clean_output: "Clean main image",
    },
    outputSlots: {
      main_output: "Taobao main image",
      clean_output: "Clean main image",
    },
  },
  "ecommerce-xiaohongshu-image-v1": {
    title: "Xiaohongshu image",
    description: "Generate a vertical lifestyle image for Xiaohongshu note covers and content seeding.",
    scenarioTitle: "Xiaohongshu",
    scenarioDescription: "For Xiaohongshu note covers, content seeding, and lifestyle display.",
    nodes: {
      style_reference: "Note style reference",
      product: "Product info",
      angle: "Cover angle",
      cover: "Vertical cover",
      cover_output: "Cover output",
      detail: "Content image",
      detail_output: "Content image output",
    },
    outputSlots: {
      cover_output: "Cover output",
      detail_output: "Content image output",
    },
    referenceInputHints: {
      style_reference: "Note style reference",
    },
  },
  "ecommerce-multi-angle-image-v1": {
    title: "Multi-angle images",
    description: "Generate front, side, and back or structure images for a complete detail gallery.",
    scenarioTitle: "Multi-angle",
    scenarioDescription: "For detail-page carousels that show appearance, structure, and back-side details.",
    nodes: {
      product: "Product info",
      angle_plan: "Angle plan",
      front_image: "Front angle",
      front_output: "Front output",
      side_image: "Side angle",
      side_output: "Side output",
      back_image: "Back / structure",
      back_output: "Structure output",
    },
    outputSlots: {
      front_output: "Front output",
      side_output: "Side output",
      back_output: "Structure output",
    },
  },
  "ecommerce-sku-variant-image-v1": {
    title: "SKU / variant images",
    description: "Generate differentiated visuals for color, specification, or bundle SKUs.",
    scenarioTitle: "SKU / variant",
    scenarioDescription: "For explaining specification differences in the product detail page.",
    nodes: {
      sku_reference: "SKU reference",
      product: "Product info",
      variant_copy: "Variant differences",
      single_variant: "Single SKU image",
      single_variant_output: "SKU image output",
      variant_grid: "Variant comparison",
      variant_grid_output: "Variant comparison output",
    },
    outputSlots: {
      single_variant_output: "SKU image output",
      variant_grid_output: "Variant comparison output",
    },
    referenceInputHints: {
      sku_reference: "SKU reference",
    },
  },
  "ecommerce-feature-infographic-v1": {
    title: "Feature infographic",
    description: "Turn core product functions into a structured infographic for detail-page persuasion.",
    scenarioTitle: "Feature highlights",
    scenarioDescription: "For detail-page feature explanation, function entry points, and conversion support.",
    nodes: {
      product: "Product info",
      feature_copy: "Benefit extraction",
      layout_copy: "Information hierarchy",
      infographic: "Feature infographic",
      infographic_output: "Feature graphic output",
    },
    outputSlots: {
      infographic_output: "Feature graphic output",
    },
  },
  "ecommerce-size-spec-image-v1": {
    title: "Size / spec image",
    description: "Generate size, capacity, material, and parameter graphics to reduce pre-purchase questions.",
    scenarioTitle: "Size / spec",
    scenarioDescription: "For explaining parameters, size, capacity, and specifications on the detail page.",
    nodes: {
      product: "Product info",
      spec_copy: "Spec organization",
      dimension_image: "Dimension annotation",
      dimension_output: "Dimension output",
      spec_table_image: "Parameter graphic",
      spec_table_output: "Parameter output",
    },
    outputSlots: {
      dimension_output: "Dimension output",
      spec_table_output: "Parameter output",
    },
  },
  "ecommerce-scale-reference-image-v1": {
    title: "Scale reference image",
    description: "Show real size through hand-held, worn, tabletop, or spatial references.",
    scenarioTitle: "Scale reference",
    scenarioDescription: "For explaining size, thickness, capacity, and real-life fit or placement.",
    nodes: {
      scale_reference: "Scale reference",
      product: "Product info",
      scale_copy: "Scale notes",
      handheld_image: "Hand-held / worn reference",
      handheld_output: "Scale image output",
      surface_image: "Tabletop / spatial reference",
      surface_output: "Spatial reference output",
    },
    outputSlots: {
      handheld_output: "Scale image output",
      surface_output: "Spatial reference output",
    },
    referenceInputHints: {
      scale_reference: "Scale reference",
    },
  },
  "ecommerce-package-checklist-image-v1": {
    title: "Package / checklist image",
    description: "Show packaging, accessories, gifts, and included items to reduce pre-sale questions.",
    scenarioTitle: "Package checklist",
    scenarioDescription: "For detail-page included items, accessory counts, and gift-box display.",
    nodes: {
      package_reference: "Packaging reference",
      product: "Product info",
      checklist_copy: "Checklist copy",
      flatlay_image: "Package flat lay",
      flatlay_output: "Checklist output",
      gift_image: "Gift-box / unboxing image",
      gift_output: "Unboxing output",
    },
    outputSlots: {
      flatlay_output: "Checklist output",
      gift_output: "Unboxing output",
    },
    referenceInputHints: {
      package_reference: "Packaging reference",
    },
  },
  "ecommerce-usage-steps-image-v1": {
    title: "Usage steps image",
    description: "Generate installation, unboxing, wearing, or cleaning step graphics.",
    scenarioTitle: "Usage steps",
    scenarioDescription: "For installation guides, tutorials, cleaning maintenance, and pre-support guidance.",
    nodes: {
      step_reference: "Step reference",
      product: "Product info",
      step_copy: "Step breakdown",
      step_image: "Step instruction graphic",
      step_output: "Step output",
      tip_copy: "Important notes",
      tip_image: "Important notes graphic",
      tip_output: "Important notes output",
    },
    outputSlots: {
      step_output: "Step output",
      tip_output: "Important notes output",
    },
    referenceInputHints: {
      step_reference: "Step reference",
    },
  },
  "ecommerce-comparison-image-v1": {
    title: "Comparison image",
    description: "Generate comparison graphics against older versions, regular versions, competitors, or bundles.",
    scenarioTitle: "Comparison",
    scenarioDescription: "For explaining upgrades, bundle differences, and purchase-decision dimensions.",
    nodes: {
      compare_reference: "Comparison reference",
      product: "Product info",
      comparison_copy: "Comparison dimensions",
      comparison_image: "Comparison graphic",
      comparison_output: "Comparison output",
      upgrade_image: "Upgrade highlights graphic",
      upgrade_output: "Upgrade highlights output",
    },
    outputSlots: {
      comparison_output: "Comparison output",
      upgrade_output: "Upgrade highlights output",
    },
    referenceInputHints: {
      compare_reference: "Comparison reference",
    },
  },
  "ecommerce-model-lifestyle-image-v1": {
    title: "Model / lifestyle image",
    description: "Generate product scene images with people, outfits, or lifestyle atmosphere.",
    scenarioTitle: "Model / lifestyle",
    scenarioDescription: "For apparel, beauty, home, and other categories that need usage context.",
    nodes: {
      style: "Pose / style reference",
      product: "Product info",
      copy: "Audience and scene",
      half_body: "Half-body / usage image",
      half_body_output: "Lifestyle image",
      detail_usage: "Usage detail image",
      detail_usage_output: "Usage detail output",
    },
    outputSlots: {
      half_body_output: "Lifestyle image",
      detail_usage_output: "Usage detail output",
    },
    referenceInputHints: {
      style: "Pose / style reference",
    },
  },
  "ecommerce-scene-image-v1": {
    title: "Scene image",
    description: "Place the product into an understandable usage space or business scene.",
    scenarioTitle: "Scene",
    scenarioDescription: "For explaining usage environment, styling, and spatial relationships.",
    nodes: {
      scene_reference: "Scene reference",
      product: "Product info",
      copy: "Scene notes",
      wide_scene: "Wide scene",
      scene_output: "Scene image output",
    },
    outputSlots: {
      scene_output: "Scene image output",
    },
    referenceInputHints: {
      scene_reference: "Scene reference",
    },
  },
  "ecommerce-detail-material-image-v1": {
    title: "Detail / material image",
    description: "Generate material, craft, local structure, or feature detail images.",
    scenarioTitle: "Detail / material",
    scenarioDescription: "For explaining material, craft, and key functions on the detail page.",
    nodes: {
      detail_reference: "Detail reference",
      product: "Product info",
      detail_copy: "Detail notes",
      macro_image: "Material close-up",
      macro_output: "Detail image output",
      structure_image: "Structure explanation graphic",
      structure_output: "Structure output",
    },
    outputSlots: {
      macro_output: "Detail image output",
      structure_output: "Structure output",
    },
    referenceInputHints: {
      detail_reference: "Detail reference",
    },
  },
  "ecommerce-campaign-promotion-image-v1": {
    title: "Campaign / promotion image",
    description: "Generate product images for campaign entry points, offer expression, and promotional atmosphere.",
    scenarioTitle: "Campaign / promotion",
    scenarioDescription: "For campaign pages, promotional placements, and in-site ad assets.",
    nodes: {
      campaign_style: "Campaign style reference",
      product: "Product info",
      offer_copy: "Offer information",
      visual_copy: "Visual hierarchy",
      banner: "Campaign banner",
      banner_output: "Campaign image output",
    },
    outputSlots: {
      banner_output: "Campaign image output",
    },
    referenceInputHints: {
      campaign_style: "Campaign style reference",
    },
  },
  "ecommerce-short-video-cover-v1": {
    title: "Short-video cover",
    description: "Generate vertical covers for short-video entry points, content feeds, and live previews.",
    scenarioTitle: "Short-video cover",
    scenarioDescription: "For in-site short videos, content feeds, live previews, and ad entry points.",
    nodes: {
      cover_style: "Cover style reference",
      product: "Product info",
      hook_copy: "Cover hook",
      frame_copy: "Frame rhythm",
      vertical_cover: "Vertical cover",
      vertical_cover_output: "Short-video cover output",
      closeup_cover: "Close-up cover",
      closeup_cover_output: "Close-up cover output",
    },
    outputSlots: {
      vertical_cover_output: "Short-video cover output",
      closeup_cover_output: "Close-up cover output",
    },
    referenceInputHints: {
      cover_style: "Cover style reference",
    },
  },
  "ecommerce-white-background-image-v1": {
    title: "White-background image",
    description: "Generate a white-background product image for marketplace rules, cutouts, or base displays.",
    scenarioTitle: "White background",
    scenarioDescription: "For platform base product images, spec graphics, and reusable assets.",
    nodes: {
      product_reference: "Subject reference",
      product: "Product info",
      clean_copy: "White-background requirements",
      white_image: "Standard white-background image",
      white_output: "White-background output",
      shadow_image: "Light-shadow display image",
      shadow_output: "Display output",
    },
    outputSlots: {
      white_output: "White-background output",
      shadow_output: "Display output",
    },
    referenceInputHints: {
      product_reference: "Subject reference",
    },
  },
};

const BUILT_IN_TEMPLATE_TEXT_JA: Record<string, BuiltInCanvasTemplateText> = {
  "ecommerce-main-image-v1": {
    title: "E-commerce メイン画像",
    description: "明確な主題、利点、構成を備えた製品のヒーロー画像を生成します。",
    scenarioTitle: "メイン画像",
    scenarioDescription: "商品一覧と詳細ページの最初の画面について。",
    nodes: {
      product: "商品情報",
      copy: "メイン画像の訴求点",
      image: "メイン画像の生成",
      output: "メイン画像出力",
      refine: "メイン画像を調整する",
      refined_output: "洗練された出力",
    },
    outputSlots: {
      output: "メイン画像出力",
      refined_output: "洗練された出力",
    },
  },
  "ecommerce-taobao-main-image-v1": {
    title: "Taobao メイン画像",
    description: "Taobao 検索、推奨事項、詳細入力用の 1:1 製品メイン画像を生成します。",
    scenarioTitle: "Taobao メイン画像",
    scenarioDescription: "Taobao の場合、トラフィックと詳細の最初の画面に、明確な件名と利点が表示されます。",
    nodes: {
      product: "商品情報",
      angle: "検索のメリット",
      main: "メインイメージのバージョン",
      main_output: "Taobao メイン画像",
      clean: "クリーンバージョン",
      clean_output: "きれいなメイン画像",
    },
    outputSlots: {
      main_output: "Taobao メイン画像",
      clean_output: "きれいなメイン画像",
    },
  },
  "ecommerce-xiaohongshu-image-v1": {
    title: "Xiaohongshu イメージ",
    description: "Xiaohongshu ノートのカバーとコンテンツのシード用に縦方向のライフスタイル画像を生成します。",
    scenarioTitle: "Xiaohongshu",
    scenarioDescription: "Xiaohongshu ノートのカバー、コンテンツのシード、ライフスタイルの表示用。",
    nodes: {
      style_reference: "ノートスタイルのリファレンス",
      product: "商品情報",
      angle: "カバーアングル",
      cover: "縦カバー",
      cover_output: "カバー出力",
      detail: "内容画像",
      detail_output: "コンテンツ画像出力",
    },
    outputSlots: {
      cover_output: "カバー出力",
      detail_output: "コンテンツ画像出力",
    },
    referenceInputHints: {
      style_reference: "ノートスタイルのリファレンス",
    },
  },
  "ecommerce-multi-angle-image-v1": {
    title: "マルチアングル画像",
    description: "完全な詳細ギャラリーの正面、側面、背面または構造画像を生成します。",
    scenarioTitle: "マルチアングル",
    scenarioDescription: "外観、構造、裏面の詳細を表示する詳細ページのカルーセル用。",
    nodes: {
      product: "商品情報",
      angle_plan: "アングルプラン",
      front_image: "フロントアングル",
      front_output: "フロント出力",
      side_image: "サイドアングル",
      side_output: "サイド出力",
      back_image: "背面・構造",
      back_output: "構造体の出力",
    },
    outputSlots: {
      front_output: "フロント出力",
      side_output: "サイド出力",
      back_output: "構造体の出力",
    },
  },
  "ecommerce-sku-variant-image-v1": {
    title: "SKU / バリアント画像",
    description: "色、仕様、またはバンドル SKU に対して差別化されたビジュアルを生成します。",
    scenarioTitle: "SKU / バリアント",
    scenarioDescription: "商品詳細ページの仕様違いを説明するため。",
    nodes: {
      sku_reference: "SKU リファレンス",
      product: "商品情報",
      variant_copy: "バリアントの違い",
      single_variant: "単一の SKU イメージ",
      single_variant_output: "SKU 画像出力",
      variant_grid: "バリアントの比較",
      variant_grid_output: "バリアント比較出力",
    },
    outputSlots: {
      single_variant_output: "SKU 画像出力",
      variant_grid_output: "バリアント比較出力",
    },
    referenceInputHints: {
      sku_reference: "SKU リファレンス",
    },
  },
  "ecommerce-feature-infographic-v1": {
    title: "機能のインフォグラフィック",
    description: "製品のコア機能を構造化されたインフォグラフィックに変換して、詳細ページでの説得力を高めます。",
    scenarioTitle: "機能のハイライト",
    scenarioDescription: "詳細ページの機能説明、関数のエントリ ポイント、変換サポートについては。",
    nodes: {
      product: "商品情報",
      feature_copy: "利益の抽出",
      layout_copy: "情報階層",
      infographic: "機能のインフォグラフィック",
      infographic_output: "フィーチャーグラフィック出力",
    },
    outputSlots: {
      infographic_output: "フィーチャーグラフィック出力",
    },
  },
  "ecommerce-size-spec-image-v1": {
    title: "サイズ・スペック画像",
    description: "サイズ、容量、材質、パラメータのグラフィックを生成して、購入前の質問を減らします。",
    scenarioTitle: "サイズ・スペック",
    scenarioDescription: "詳細ページのパラメータ、サイズ、容量、仕様を説明するため。",
    nodes: {
      product: "商品情報",
      spec_copy: "スペック構成",
      dimension_image: "寸法の注釈",
      dimension_output: "寸法出力",
      spec_table_image: "パラメータグラフィック",
      spec_table_output: "パラメータ出力",
    },
    outputSlots: {
      dimension_output: "寸法出力",
      spec_table_output: "パラメータ出力",
    },
  },
  "ecommerce-scale-reference-image-v1": {
    title: "スケール参考画像",
    description: "手持ち、着用、卓上、または空間参照を通じて実際のサイズを表示します。",
    scenarioTitle: "スケールリファレンス",
    scenarioDescription: "サイズ、厚さ、容量、実際のフィット感や配置を説明するため。",
    nodes: {
      scale_reference: "スケールリファレンス",
      product: "商品情報",
      scale_copy: "スケールノート",
      handheld_image: "手持ち/着用リファレンス",
      handheld_output: "スケール画像出力",
      surface_image: "卓上/空間参照",
      surface_output: "空間参照出力",
    },
    outputSlots: {
      handheld_output: "スケール画像出力",
      surface_output: "空間参照出力",
    },
    referenceInputHints: {
      scale_reference: "スケールリファレンス",
    },
  },
  "ecommerce-package-checklist-image-v1": {
    title: "パッケージ・チェックリストイメージ",
    description: "販売前の質問を減らすために、パッケージ、付属品、ギフト、同梱品を表示します。",
    scenarioTitle: "パッケージチェックリスト",
    scenarioDescription: "詳細ページの同梱品、付属品数、ギフトボックスの表示について。",
    nodes: {
      package_reference: "パッケージングリファレンス",
      product: "商品情報",
      checklist_copy: "チェックリストのコピー",
      flatlay_image: "パッケージ平置き",
      flatlay_output: "チェックリストの出力",
      gift_image: "ギフト箱・開封イメージ",
      gift_output: "開梱出力",
    },
    outputSlots: {
      flatlay_output: "チェックリストの出力",
      gift_output: "開梱出力",
    },
    referenceInputHints: {
      package_reference: "パッケージングリファレンス",
    },
  },
  "ecommerce-usage-steps-image-v1": {
    title: "利用手順イメージ",
    description: "取り付け、開梱、着用、またはクリーニングのステップグラフィックを生成します。",
    scenarioTitle: "利用手順",
    scenarioDescription: "インストール ガイド、チュートリアル、クリーニング メンテナンス、およびプレサポート ガイダンスについては。",
    nodes: {
      step_reference: "ステップリファレンス",
      product: "商品情報",
      step_copy: "ステップの内訳",
      step_image: "ステップ説明図",
      step_output: "ステップ出力",
      tip_copy: "重要な注意事項",
      tip_image: "重要な注意事項のグラフィック",
      tip_output: "重要なメモの出力",
    },
    outputSlots: {
      step_output: "ステップ出力",
      tip_output: "重要なメモの出力",
    },
    referenceInputHints: {
      step_reference: "ステップリファレンス",
    },
  },
  "ecommerce-comparison-image-v1": {
    title: "比較画像",
    description: "古いバージョン、通常バージョン、競合製品、またはバンドルとの比較グラフィックを生成します。",
    scenarioTitle: "比較",
    scenarioDescription: "アップグレード、バンドルの違い、購入決定の要素について説明します。",
    nodes: {
      compare_reference: "比較参考",
      product: "商品情報",
      comparison_copy: "比較寸法",
      comparison_image: "比較図",
      comparison_output: "比較出力",
      upgrade_image: "アップグレードのハイライトグラフィック",
      upgrade_output: "アップグレードのハイライト出力",
    },
    outputSlots: {
      comparison_output: "比較出力",
      upgrade_output: "アップグレードのハイライト出力",
    },
    referenceInputHints: {
      compare_reference: "比較参考",
    },
  },
  "ecommerce-model-lifestyle-image-v1": {
    title: "モデル・ライフスタイルイメージ",
    description: "人物、衣装、ライフスタイルの雰囲気を取り入れた商品シーンの画像を生成します。",
    scenarioTitle: "モデル・ライフスタイル",
    scenarioDescription: "アパレル、美容、家庭、その他使用コンテキストが必要なカテゴリ向け。",
    nodes: {
      style: "ポーズ・スタイル参考",
      product: "商品情報",
      copy: "観客とシーン",
      half_body: "半身・使用イメージ",
      half_body_output: "ライフスタイルイメージ",
      detail_usage: "ご利用詳細イメージ",
      detail_usage_output: "使用状況の詳細出力",
    },
    outputSlots: {
      half_body_output: "ライフスタイルイメージ",
      detail_usage_output: "使用状況の詳細出力",
    },
    referenceInputHints: {
      style: "ポーズ・スタイル参考",
    },
  },
  "ecommerce-scene-image-v1": {
    title: "場面写真",
    description: "わかりやすい利用空間やビジネスシーンに設置してください。",
    scenarioTitle: "シーン",
    scenarioDescription: "使用環境やスタイリング、空間関係の説明に。",
    nodes: {
      scene_reference: "シーンリファレンス",
      product: "商品情報",
      copy: "シーンノート",
      wide_scene: "幅広いシーン",
      scene_output: "シーン映像出力",
    },
    outputSlots: {
      scene_output: "シーン映像出力",
    },
    referenceInputHints: {
      scene_reference: "シーンリファレンス",
    },
  },
  "ecommerce-detail-material-image-v1": {
    title: "詳細・素材画像",
    description: "マテリアル、クラフト、ローカル構造、またはフィーチャの詳細画像を生成します。",
    scenarioTitle: "ディテール・素材",
    scenarioDescription: "詳細ページの素材、クラフト、キー機能の説明用。",
    nodes: {
      detail_reference: "詳細参照",
      product: "商品情報",
      detail_copy: "詳細メモ",
      macro_image: "素材のクローズアップ",
      macro_output: "詳細画像出力",
      structure_image: "構造説明図",
      structure_output: "構造体の出力",
    },
    outputSlots: {
      macro_output: "詳細画像出力",
      structure_output: "構造体の出力",
    },
    referenceInputHints: {
      detail_reference: "詳細参照",
    },
  },
  "ecommerce-campaign-promotion-image-v1": {
    title: "キャンペーン・プロモーションイメージ",
    description: "キャンペーンのエントリーポイント、オファーの表現、プロモーションの雰囲気のための製品画像を生成します。",
    scenarioTitle: "キャンペーン・プロモーション",
    scenarioDescription: "キャンペーン ページ、プロモーションの配置、サイト内広告アセットの場合。",
    nodes: {
      campaign_style: "キャンペーンスタイルのリファレンス",
      product: "商品情報",
      offer_copy: "オファー情報",
      visual_copy: "視覚的な階層",
      banner: "キャンペーンバナー",
      banner_output: "キャンペーン画像出力",
    },
    outputSlots: {
      banner_output: "キャンペーン画像出力",
    },
    referenceInputHints: {
      campaign_style: "キャンペーンスタイルのリファレンス",
    },
  },
  "ecommerce-short-video-cover-v1": {
    title: "ショートビデオのカバー",
    description: "ショートビデオのエントリーポイント、コンテンツフィード、ライブプレビュー用の垂直カバーを生成します。",
    scenarioTitle: "ショートビデオのカバー",
    scenarioDescription: "サイト内の短いビデオ、コンテンツ フィード、ライブ プレビュー、広告エントリ ポイント用。",
    nodes: {
      cover_style: "カバースタイルのリファレンス",
      product: "商品情報",
      hook_copy: "カバーフック",
      frame_copy: "フレームリズム",
      vertical_cover: "縦カバー",
      vertical_cover_output: "ショートビデオカバー出力",
      closeup_cover: "クローズアップカバー",
      closeup_cover_output: "クローズアップカバー出力",
    },
    outputSlots: {
      vertical_cover_output: "ショートビデオカバー出力",
      closeup_cover_output: "クローズアップカバー出力",
    },
    referenceInputHints: {
      cover_style: "カバースタイルのリファレンス",
    },
  },
  "ecommerce-white-background-image-v1": {
    title: "白背景画像",
    description: "マーケットプレイスのルール、カットアウト、またはベースディスプレイ用に白背景の製品画像を生成します。",
    scenarioTitle: "白い背景",
    scenarioDescription: "プラットフォームベースの製品画像、仕様グラフィックス、再利用可能なアセット用。",
    nodes: {
      product_reference: "件名参照",
      product: "商品情報",
      clean_copy: "白背景の要件",
      white_image: "標準白背景画像",
      white_output: "白背景出力",
      shadow_image: "明暗表示イメージ",
      shadow_output: "表示出力",
    },
    outputSlots: {
      white_output: "白背景出力",
      shadow_output: "表示出力",
    },
    referenceInputHints: {
      product_reference: "件名参照",
    },
  },
};

const BUILT_IN_TEMPLATE_TEXT_VI: Record<string, BuiltInCanvasTemplateText> = {
  "ecommerce-main-image-v1": {
    title: "Ảnh chính thương mại điện tử",
    description: "Tạo ảnh nổi bật về sản phẩm với chủ đề, lợi ích và bố cục rõ ràng.",
    scenarioTitle: "Ảnh chính",
    scenarioDescription: "Dành cho listing sản phẩm và màn hình đầu tiên của trang chi tiết.",
    nodes: {
      product: "Thông tin sản phẩm",
      copy: "Lợi ích của ảnh chính",
      image: "Tạo ảnh chính",
      output: "Đầu ra ảnh chính",
      refine: "Tinh chỉnh ảnh chính",
      refined_output: "Đầu ra tinh tế",
    },
    outputSlots: {
      output: "Đầu ra ảnh chính",
      refined_output: "Đầu ra tinh tế",
    },
  },
  "ecommerce-taobao-main-image-v1": {
    title: "Taobao ảnh chính",
    description: "Tạo ảnh chính của sản phẩm 1:1 cho Taobao tìm kiếm, đề xuất và nhập chi tiết.",
    scenarioTitle: "Taobao ảnh chính",
    scenarioDescription: "Đối với Taobao liệt kê lưu lượng truy cập và màn hình chi tiết đầu tiên, có chủ đề và lợi ích rõ ràng.",
    nodes: {
      product: "Thông tin sản phẩm",
      angle: "Tìm kiếm lợi ích",
      main: "Phiên bản ảnh chính",
      main_output: "Taobao ảnh chính",
      clean: "Phiên bản sạch",
      clean_output: "Làm sạch ảnh chính",
    },
    outputSlots: {
      main_output: "Taobao ảnh chính",
      clean_output: "Làm sạch ảnh chính",
    },
  },
  "ecommerce-xiaohongshu-image-v1": {
    title: "Xiaohongshu ảnh",
    description: "Tạo ảnh phong cách sống theo chiều dọc cho bìa ghi chú Xiaohongshu và seeding nội dung.",
    scenarioTitle: "Xiaohongshu",
    scenarioDescription: "Dành cho bìa ghi chú Xiaohongshu, seeding nội dung và hiển thị phong cách sống.",
    nodes: {
      style_reference: "Tham khảo kiểu ghi chú",
      product: "Thông tin sản phẩm",
      angle: "Góc che",
      cover: "Bìa dọc",
      cover_output: "Đầu ra bìa",
      detail: "Ảnh nội dung",
      detail_output: "Đầu ra ảnh nội dung",
    },
    outputSlots: {
      cover_output: "Đầu ra bìa",
      detail_output: "Đầu ra ảnh nội dung",
    },
    referenceInputHints: {
      style_reference: "Tham khảo kiểu ghi chú",
    },
  },
  "ecommerce-multi-angle-image-v1": {
    title: "Ảnh đa góc độ",
    description: "Tạo ảnh mặt trước, mặt bên và mặt sau hoặc cấu trúc để có bộ sưu tập chi tiết hoàn chỉnh.",
    scenarioTitle: "Đa góc",
    scenarioDescription: "Dành cho băng chuyền trang chi tiết hiển thị hình thức, cấu trúc và chi tiết mặt sau.",
    nodes: {
      product: "Thông tin sản phẩm",
      angle_plan: "Sơ đồ góc",
      front_image: "Góc trước",
      front_output: "Đầu ra phía trước",
      side_image: "Góc bên",
      side_output: "Đầu ra bên",
      back_image: "Trở lại / cấu trúc",
      back_output: "Kết cấu đầu ra",
    },
    outputSlots: {
      front_output: "Đầu ra phía trước",
      side_output: "Đầu ra bên",
      back_output: "Kết cấu đầu ra",
    },
  },
  "ecommerce-sku-variant-image-v1": {
    title: "SKU / ảnh biến thể",
    description: "Tạo ảnh khác biệt về màu sắc, thông số kỹ thuật hoặc gói SKUs.",
    scenarioTitle: "SKU / biến thể",
    scenarioDescription: "Để giải thích sự khác biệt về thông số kỹ thuật trong trang chi tiết sản phẩm.",
    nodes: {
      sku_reference: "SKU tài liệu tham khảo",
      product: "Thông tin sản phẩm",
      variant_copy: "Sự khác biệt về biến thể",
      single_variant: "Ảnh đơn SKU",
      single_variant_output: "Đầu ra ảnh SKU",
      variant_grid: "So sánh biến thể",
      variant_grid_output: "Đầu ra so sánh biến thể",
    },
    outputSlots: {
      single_variant_output: "Đầu ra ảnh SKU",
      variant_grid_output: "Đầu ra so sánh biến thể",
    },
    referenceInputHints: {
      sku_reference: "SKU tài liệu tham khảo",
    },
  },
  "ecommerce-feature-infographic-v1": {
    title: "đồ họa thông tin nổi bật",
    description: "Biến các chức năng cốt lõi của sản phẩm thành đồ họa thông tin có cấu trúc để thuyết phục trang chi tiết.",
    scenarioTitle: "Tính năng nổi bật",
    scenarioDescription: "Để biết giải thích về tính năng trang chi tiết, điểm nhập chức năng và hỗ trợ chuyển đổi.",
    nodes: {
      product: "Thông tin sản phẩm",
      feature_copy: "Khai thác lợi ích",
      layout_copy: "Hệ thống phân cấp thông tin",
      infographic: "đồ họa thông tin nổi bật",
      infographic_output: "Đầu ra đồ họa nổi bật",
    },
    outputSlots: {
      infographic_output: "Đầu ra đồ họa nổi bật",
    },
  },
  "ecommerce-size-spec-image-v1": {
    title: "Kích thước/thông số ảnh",
    description: "Tạo đồ họa kích thước, công suất, vật liệu và thông số để giảm bớt các câu hỏi trước khi mua.",
    scenarioTitle: "Kích thước/thông số kỹ thuật",
    scenarioDescription: "Để giải thích các thông số, kích thước, công suất và thông số kỹ thuật trên trang chi tiết.",
    nodes: {
      product: "Thông tin sản phẩm",
      spec_copy: "Tổ chức đặc tả",
      dimension_image: "Chú thích thứ nguyên",
      dimension_output: "Đầu ra kích thước",
      spec_table_image: "đồ họa thông số",
      spec_table_output: "Đầu ra tham số",
    },
    outputSlots: {
      dimension_output: "Đầu ra kích thước",
      spec_table_output: "Đầu ra tham số",
    },
  },
  "ecommerce-scale-reference-image-v1": {
    title: "Ảnh tham khảo tỷ lệ",
    description: "Hiển thị kích thước thực thông qua các tài liệu tham khảo cầm tay, đeo trên mặt bàn hoặc không gian.",
    scenarioTitle: "Tham khảo tỷ lệ",
    scenarioDescription: "Để giải thích kích thước, độ dày, công suất và sự phù hợp hoặc vị trí trong đời thực.",
    nodes: {
      scale_reference: "Tham khảo tỷ lệ",
      product: "Thông tin sản phẩm",
      scale_copy: "nốt nhạc quy mô",
      handheld_image: "Tài liệu tham khảo cầm tay / đeo",
      handheld_output: "Đầu ra ảnh tỷ lệ",
      surface_image: "Tham chiếu mặt bàn/không gian",
      surface_output: "Đầu ra tham chiếu không gian",
    },
    outputSlots: {
      handheld_output: "Đầu ra ảnh tỷ lệ",
      surface_output: "Đầu ra tham chiếu không gian",
    },
    referenceInputHints: {
      scale_reference: "Tham khảo tỷ lệ",
    },
  },
  "ecommerce-package-checklist-image-v1": {
    title: "Ảnh gói hàng/listing kiểm tra",
    description: "Hiển thị bao bì, phụ kiện, quà tặng và các mặt hàng đi kèm để giảm bớt thắc mắc trước khi bán.",
    scenarioTitle: "Danh sách kiểm tra gói hàng",
    scenarioDescription: "Đối với các mặt hàng có trong trang chi tiết, số lượng phụ kiện và cách hiển thị hộp quà tặng.",
    nodes: {
      package_reference: "Tham khảo bao bì",
      product: "Thông tin sản phẩm",
      checklist_copy: "Bản sao listing kiểm tra",
      flatlay_image: "Gói nằm phẳng",
      flatlay_output: "Đầu ra listing kiểm tra",
      gift_image: "Ảnh hộp quà/khui hộp",
      gift_output: "Đầu ra mở hộp",
    },
    outputSlots: {
      flatlay_output: "Đầu ra listing kiểm tra",
      gift_output: "Đầu ra mở hộp",
    },
    referenceInputHints: {
      package_reference: "Tham khảo bao bì",
    },
  },
  "ecommerce-usage-steps-image-v1": {
    title: "Ảnh các bước sử dụng",
    description: "Tạo đồ họa bước cài đặt, mở hộp, mặc hoặc làm sạch.",
    scenarioTitle: "Các bước sử dụng",
    scenarioDescription: "Để biết hướng dẫn cài đặt, hướng dẫn, bảo trì vệ sinh và hướng dẫn hỗ trợ trước.",
    nodes: {
      step_reference: "Tham khảo bước",
      product: "Thông tin sản phẩm",
      step_copy: "Phân tích bước",
      step_image: "Ảnh hướng dẫn bước",
      step_output: "Bước đầu ra",
      tip_copy: "Ghi chú quan trọng",
      tip_image: "Đồ họa ghi chú quan trọng",
      tip_output: "Đầu ra ghi chú quan trọng",
    },
    outputSlots: {
      step_output: "Bước đầu ra",
      tip_output: "Đầu ra ghi chú quan trọng",
    },
    referenceInputHints: {
      step_reference: "Tham khảo bước",
    },
  },
  "ecommerce-comparison-image-v1": {
    title: "Ảnh so sánh",
    description: "Tạo đồ họa so sánh với các phiên bản cũ hơn, phiên bản thông thường, đối thủ cạnh tranh hoặc gói.",
    scenarioTitle: "So sánh",
    scenarioDescription: "Để giải thích các nâng cấp, sự khác biệt của gói và kích thước quyết định mua hàng.",
    nodes: {
      compare_reference: "Tham khảo so sánh",
      product: "Thông tin sản phẩm",
      comparison_copy: "Kích thước so sánh",
      comparison_image: "Đồ họa so sánh",
      comparison_output: "Đầu ra so sánh",
      upgrade_image: "Nâng cấp đồ họa nổi bật",
      upgrade_output: "Nâng cấp đầu ra nổi bật",
    },
    outputSlots: {
      comparison_output: "Đầu ra so sánh",
      upgrade_output: "Nâng cấp đầu ra nổi bật",
    },
    referenceInputHints: {
      compare_reference: "Tham khảo so sánh",
    },
  },
  "ecommerce-model-lifestyle-image-v1": {
    title: "Ảnh model/phong cách sống",
    description: "Tạo ảnh cảnh sản phẩm với con người, trang phục hoặc bầu không khí phong cách sống.",
    scenarioTitle: "Người mẫu/lối sống",
    scenarioDescription: "Dành cho quần áo, làm đẹp, nhà cửa và các danh mục khác cần bối cảnh sử dụng.",
    nodes: {
      style: "Tham khảo tư thế/phong cách",
      product: "Thông tin sản phẩm",
      copy: "Khán giả và bối cảnh",
      half_body: "Ảnh nửa thân/cách sử dụng",
      half_body_output: "Ảnh phong cách sống",
      detail_usage: "Ảnh chi tiết sử dụng",
      detail_usage_output: "Đầu ra chi tiết sử dụng",
    },
    outputSlots: {
      half_body_output: "Ảnh phong cách sống",
      detail_usage_output: "Đầu ra chi tiết sử dụng",
    },
    referenceInputHints: {
      style: "Tham khảo tư thế/phong cách",
    },
  },
  "ecommerce-scene-image-v1": {
    title: "Ảnh cảnh",
    description: "Đặt sản phẩm vào một không gian sử dụng hoặc bối cảnh kinh doanh dễ hiểu.",
    scenarioTitle: "Cảnh",
    scenarioDescription: "Để giải thích môi trường sử dụng, kiểu dáng và các mối quan hệ không gian.",
    nodes: {
      scene_reference: "Tham chiếu cảnh",
      product: "Thông tin sản phẩm",
      copy: "Ghi chú cảnh",
      wide_scene: "Cảnh rộng",
      scene_output: "Đầu ra ảnh cảnh",
    },
    outputSlots: {
      scene_output: "Đầu ra ảnh cảnh",
    },
    referenceInputHints: {
      scene_reference: "Tham chiếu cảnh",
    },
  },
  "ecommerce-detail-material-image-v1": {
    title: "Ảnh chi tiết/chất liệu",
    description: "Tạo vật liệu, thủ công, cấu trúc cục bộ hoặc ảnh chi tiết về đặc điểm.",
    scenarioTitle: "Chi tiết/chất liệu",
    scenarioDescription: "Để giải thích các chức năng vật liệu, thủ công và chính trên trang chi tiết.",
    nodes: {
      detail_reference: "Tham khảo chi tiết",
      product: "Thông tin sản phẩm",
      detail_copy: "Ghi chú chi tiết",
      macro_image: "Cận cảnh chất liệu",
      macro_output: "Đầu ra ảnh chi tiết",
      structure_image: "Đồ họa giải thích cấu trúc",
      structure_output: "Kết cấu đầu ra",
    },
    outputSlots: {
      macro_output: "Đầu ra ảnh chi tiết",
      structure_output: "Kết cấu đầu ra",
    },
    referenceInputHints: {
      detail_reference: "Tham khảo chi tiết",
    },
  },
  "ecommerce-campaign-promotion-image-v1": {
    title: "Ảnh chiến dịch/quảng cáo",
    description: "Tạo ảnh sản phẩm cho các điểm tham gia chiến dịch, biểu hiện ưu đãi và không khí quảng cáo.",
    scenarioTitle: "Chiến dịch/khuyến mãi",
    scenarioDescription: "Dành cho các trang chiến dịch, vị trí quảng cáo và nội dung quảng cáo trong trang web.",
    nodes: {
      campaign_style: "Tham chiếu kiểu chiến dịch",
      product: "Thông tin sản phẩm",
      offer_copy: "Thông tin ưu đãi",
      visual_copy: "Phân cấp trực quan",
      banner: "Biểu ngữ chiến dịch",
      banner_output: "Đầu ra ảnh chiến dịch",
    },
    outputSlots: {
      banner_output: "Đầu ra ảnh chiến dịch",
    },
    referenceInputHints: {
      campaign_style: "Tham chiếu kiểu chiến dịch",
    },
  },
  "ecommerce-short-video-cover-v1": {
    title: "Bìa video ngắn",
    description: "Tạo bìa dọc cho các điểm nhập video ngắn, nguồn cấp nội dung và bản xem trước trực tiếp.",
    scenarioTitle: "Bìa video ngắn",
    scenarioDescription: "Dành cho các video ngắn, nguồn cấp nội dung, bản xem trước trực tiếp và điểm nhập quảng cáo trong trang web.",
    nodes: {
      cover_style: "Tham khảo kiểu bìa",
      product: "Thông tin sản phẩm",
      hook_copy: "Móc che",
      frame_copy: "Nhịp điệu khung",
      vertical_cover: "Bìa dọc",
      vertical_cover_output: "Đầu ra bìa video ngắn",
      closeup_cover: "Bìa cận cảnh",
      closeup_cover_output: "Đầu ra bìa cận cảnh",
    },
    outputSlots: {
      vertical_cover_output: "Đầu ra bìa video ngắn",
      closeup_cover_output: "Đầu ra bìa cận cảnh",
    },
    referenceInputHints: {
      cover_style: "Tham khảo kiểu bìa",
    },
  },
  "ecommerce-white-background-image-v1": {
    title: "Ảnh nền trắng",
    description: "Tạo ảnh sản phẩm nền trắng cho các quy tắc thị trường, phần cắt ra hoặc màn hình cơ sở.",
    scenarioTitle: "Nền trắng",
    scenarioDescription: "Dành cho ảnh sản phẩm cơ sở nền tảng, đồ họa thông số kỹ thuật và nội dung có thể tái sử dụng.",
    nodes: {
      product_reference: "Chủ đề tham khảo",
      product: "Thông tin sản phẩm",
      clean_copy: "Yêu cầu nền trắng",
      white_image: "Ảnh nền trắng chuẩn",
      white_output: "Đầu ra nền trắng",
      shadow_image: "Ảnh hiển thị bóng sáng",
      shadow_output: "Hiển thị đầu ra",
    },
    outputSlots: {
      white_output: "Đầu ra nền trắng",
      shadow_output: "Hiển thị đầu ra",
    },
    referenceInputHints: {
      product_reference: "Chủ đề tham khảo",
    },
  },
};

const BUILT_IN_TEMPLATE_TEXT_BY_LOCALE: Partial<Record<Locale, Record<string, BuiltInCanvasTemplateText>>> = {
  "en-US": BUILT_IN_TEMPLATE_TEXT_EN,
  "ja-JP": BUILT_IN_TEMPLATE_TEXT_JA,
  "vi-VN": BUILT_IN_TEMPLATE_TEXT_VI,
};

const BUILT_IN_TEMPLATE_SOURCE_TEXT: Record<
  string,
  {
    nodes: Record<string, { nodeType: WorkflowNodeType; title: string }>;
    outputSlots: Record<string, string>;
    referenceInputHints?: Record<string, string>;
    defaultExternalConnections?: Record<string, string>;
  }
> = {
  "ecommerce-main-image-v1": {
    nodes: {
      product: { nodeType: "product_context", title: "商品资料" },
      copy: { nodeType: "copy_generation", title: "主图卖点" },
      image: { nodeType: "image_generation", title: "生成主图" },
      output: { nodeType: "reference_image", title: "主图输出" },
      refine: { nodeType: "image_generation", title: "细化主图" },
      refined_output: { nodeType: "reference_image", title: "细化输出" },
    },
    outputSlots: { output: "主图输出", refined_output: "细化输出" },
  },
  "ecommerce-taobao-main-image-v1": {
    nodes: {
      product: { nodeType: "product_context", title: "商品资料" },
      angle: { nodeType: "copy_generation", title: "搜索卖点" },
      main: { nodeType: "image_generation", title: "主图版本" },
      main_output: { nodeType: "reference_image", title: "淘宝主图" },
      clean: { nodeType: "image_generation", title: "干净版本" },
      clean_output: { nodeType: "reference_image", title: "干净主图" },
    },
    outputSlots: { main_output: "淘宝主图", clean_output: "干净主图" },
  },
  "ecommerce-xiaohongshu-image-v1": {
    nodes: {
      style_reference: { nodeType: "reference_image", title: "笔记风格参考" },
      product: { nodeType: "product_context", title: "商品资料" },
      angle: { nodeType: "copy_generation", title: "封面角度" },
      cover: { nodeType: "image_generation", title: "竖版封面" },
      cover_output: { nodeType: "reference_image", title: "封面输出" },
      detail: { nodeType: "image_generation", title: "内容配图" },
      detail_output: { nodeType: "reference_image", title: "配图输出" },
    },
    outputSlots: { cover_output: "封面输出", detail_output: "配图输出" },
    referenceInputHints: { style_reference: "笔记风格参考" },
  },
  "ecommerce-multi-angle-image-v1": {
    nodes: {
      product: { nodeType: "product_context", title: "商品资料" },
      angle_plan: { nodeType: "copy_generation", title: "角度规划" },
      front_image: { nodeType: "image_generation", title: "正面角度" },
      front_output: { nodeType: "reference_image", title: "正面输出" },
      side_image: { nodeType: "image_generation", title: "侧面角度" },
      side_output: { nodeType: "reference_image", title: "侧面输出" },
      back_image: { nodeType: "image_generation", title: "背面/结构" },
      back_output: { nodeType: "reference_image", title: "结构输出" },
    },
    outputSlots: { front_output: "正面输出", side_output: "侧面输出", back_output: "结构输出" },
  },
  "ecommerce-sku-variant-image-v1": {
    nodes: {
      sku_reference: { nodeType: "reference_image", title: "SKU 参考图" },
      product: { nodeType: "product_context", title: "商品资料" },
      variant_copy: { nodeType: "copy_generation", title: "变体差异" },
      single_variant: { nodeType: "image_generation", title: "单 SKU 图" },
      single_variant_output: { nodeType: "reference_image", title: "SKU 图输出" },
      variant_grid: { nodeType: "image_generation", title: "变体对照" },
      variant_grid_output: { nodeType: "reference_image", title: "变体对照输出" },
    },
    outputSlots: { single_variant_output: "SKU 图输出", variant_grid_output: "变体对照输出" },
    referenceInputHints: { sku_reference: "SKU 参考图" },
  },
  "ecommerce-feature-infographic-v1": {
    nodes: {
      product: { nodeType: "product_context", title: "商品资料" },
      feature_copy: { nodeType: "copy_generation", title: "卖点提炼" },
      layout_copy: { nodeType: "copy_generation", title: "信息层级" },
      infographic: { nodeType: "image_generation", title: "卖点信息图" },
      infographic_output: { nodeType: "reference_image", title: "卖点图输出" },
    },
    outputSlots: { infographic_output: "卖点图输出" },
  },
  "ecommerce-size-spec-image-v1": {
    nodes: {
      product: { nodeType: "product_context", title: "商品资料" },
      spec_copy: { nodeType: "copy_generation", title: "规格整理" },
      dimension_image: { nodeType: "image_generation", title: "尺寸标注图" },
      dimension_output: { nodeType: "reference_image", title: "尺寸输出" },
      spec_table_image: { nodeType: "image_generation", title: "参数说明图" },
      spec_table_output: { nodeType: "reference_image", title: "参数输出" },
    },
    outputSlots: { dimension_output: "尺寸输出", spec_table_output: "参数输出" },
  },
  "ecommerce-scale-reference-image-v1": {
    nodes: {
      scale_reference: { nodeType: "reference_image", title: "参照物参考" },
      product: { nodeType: "product_context", title: "商品资料" },
      scale_copy: { nodeType: "copy_generation", title: "尺度说明" },
      handheld_image: { nodeType: "image_generation", title: "手持/佩戴参照" },
      handheld_output: { nodeType: "reference_image", title: "尺度图输出" },
      surface_image: { nodeType: "image_generation", title: "桌面/空间参照" },
      surface_output: { nodeType: "reference_image", title: "空间参照输出" },
    },
    outputSlots: { handheld_output: "尺度图输出", surface_output: "空间参照输出" },
    referenceInputHints: { scale_reference: "参照物参考" },
  },
  "ecommerce-package-checklist-image-v1": {
    nodes: {
      package_reference: { nodeType: "reference_image", title: "包装参考" },
      product: { nodeType: "product_context", title: "商品资料" },
      checklist_copy: { nodeType: "copy_generation", title: "清单文案" },
      flatlay_image: { nodeType: "image_generation", title: "包装平铺图" },
      flatlay_output: { nodeType: "reference_image", title: "清单输出" },
      gift_image: { nodeType: "image_generation", title: "礼盒/到手图" },
      gift_output: { nodeType: "reference_image", title: "到手输出" },
    },
    outputSlots: { flatlay_output: "清单输出", gift_output: "到手输出" },
    referenceInputHints: { package_reference: "包装参考" },
  },
  "ecommerce-usage-steps-image-v1": {
    nodes: {
      step_reference: { nodeType: "reference_image", title: "步骤参考" },
      product: { nodeType: "product_context", title: "商品资料" },
      step_copy: { nodeType: "copy_generation", title: "步骤拆解" },
      step_image: { nodeType: "image_generation", title: "步骤说明图" },
      step_output: { nodeType: "reference_image", title: "步骤输出" },
      tip_copy: { nodeType: "copy_generation", title: "注意事项" },
      tip_image: { nodeType: "image_generation", title: "注意事项图" },
      tip_output: { nodeType: "reference_image", title: "注意事项输出" },
    },
    outputSlots: { step_output: "步骤输出", tip_output: "注意事项输出" },
    referenceInputHints: { step_reference: "步骤参考" },
  },
  "ecommerce-comparison-image-v1": {
    nodes: {
      compare_reference: { nodeType: "reference_image", title: "对比参考" },
      product: { nodeType: "product_context", title: "商品资料" },
      comparison_copy: { nodeType: "copy_generation", title: "对比维度" },
      comparison_image: { nodeType: "image_generation", title: "对比说明图" },
      comparison_output: { nodeType: "reference_image", title: "对比输出" },
      upgrade_image: { nodeType: "image_generation", title: "升级点图" },
      upgrade_output: { nodeType: "reference_image", title: "升级点输出" },
    },
    outputSlots: { comparison_output: "对比输出", upgrade_output: "升级点输出" },
    referenceInputHints: { compare_reference: "对比参考" },
  },
  "ecommerce-model-lifestyle-image-v1": {
    nodes: {
      style: { nodeType: "reference_image", title: "姿态/风格参考" },
      product: { nodeType: "product_context", title: "商品资料" },
      copy: { nodeType: "copy_generation", title: "人群与场景" },
      half_body: { nodeType: "image_generation", title: "半身/使用图" },
      half_body_output: { nodeType: "reference_image", title: "生活方式图" },
      detail_usage: { nodeType: "image_generation", title: "使用细节图" },
      detail_usage_output: { nodeType: "reference_image", title: "使用细节输出" },
    },
    outputSlots: { half_body_output: "生活方式图", detail_usage_output: "使用细节输出" },
    referenceInputHints: { style: "姿态/风格参考" },
  },
  "ecommerce-scene-image-v1": {
    nodes: {
      scene_reference: { nodeType: "reference_image", title: "场景参考" },
      product: { nodeType: "product_context", title: "商品资料" },
      copy: { nodeType: "copy_generation", title: "场景说明" },
      wide_scene: { nodeType: "image_generation", title: "宽幅场景" },
      scene_output: { nodeType: "reference_image", title: "场景图输出" },
    },
    outputSlots: { scene_output: "场景图输出" },
    referenceInputHints: { scene_reference: "场景参考" },
  },
  "ecommerce-detail-material-image-v1": {
    nodes: {
      detail_reference: { nodeType: "reference_image", title: "细节参考图" },
      product: { nodeType: "product_context", title: "商品资料" },
      detail_copy: { nodeType: "copy_generation", title: "细节说明" },
      macro_image: { nodeType: "image_generation", title: "材质特写" },
      macro_output: { nodeType: "reference_image", title: "细节图输出" },
      structure_image: { nodeType: "image_generation", title: "结构说明图" },
      structure_output: { nodeType: "reference_image", title: "结构输出" },
    },
    outputSlots: { macro_output: "细节图输出", structure_output: "结构输出" },
    referenceInputHints: { detail_reference: "细节参考图" },
  },
  "ecommerce-campaign-promotion-image-v1": {
    nodes: {
      campaign_style: { nodeType: "reference_image", title: "活动风格参考" },
      product: { nodeType: "product_context", title: "商品资料" },
      offer_copy: { nodeType: "copy_generation", title: "优惠信息" },
      visual_copy: { nodeType: "copy_generation", title: "视觉层级" },
      banner: { nodeType: "image_generation", title: "活动横图" },
      banner_output: { nodeType: "reference_image", title: "活动图输出" },
    },
    outputSlots: { banner_output: "活动图输出" },
    referenceInputHints: { campaign_style: "活动风格参考" },
  },
  "ecommerce-short-video-cover-v1": {
    nodes: {
      cover_style: { nodeType: "reference_image", title: "封面风格参考" },
      product: { nodeType: "product_context", title: "商品资料" },
      hook_copy: { nodeType: "copy_generation", title: "封面钩子" },
      frame_copy: { nodeType: "copy_generation", title: "画面节奏" },
      vertical_cover: { nodeType: "image_generation", title: "竖版封面" },
      vertical_cover_output: { nodeType: "reference_image", title: "短视频封面输出" },
      closeup_cover: { nodeType: "image_generation", title: "特写封面" },
      closeup_cover_output: { nodeType: "reference_image", title: "特写封面输出" },
    },
    outputSlots: {
      vertical_cover_output: "短视频封面输出",
      closeup_cover_output: "特写封面输出",
    },
    referenceInputHints: { cover_style: "封面风格参考" },
  },
  "ecommerce-white-background-image-v1": {
    nodes: {
      product_reference: { nodeType: "reference_image", title: "主体参考图" },
      product: { nodeType: "product_context", title: "商品资料" },
      clean_copy: { nodeType: "copy_generation", title: "白底要求" },
      white_image: { nodeType: "image_generation", title: "标准白底图" },
      white_output: { nodeType: "reference_image", title: "白底图输出" },
      shadow_image: { nodeType: "image_generation", title: "轻阴影陈列图" },
      shadow_output: { nodeType: "reference_image", title: "陈列输出" },
    },
    outputSlots: { white_output: "白底图输出", shadow_output: "陈列输出" },
    referenceInputHints: { product_reference: "主体参考图" },
  },
};

const BUILT_IN_NODE_TITLE_BY_LOCALE = new Map<Locale, Map<WorkflowNodeType, Map<string, string>>>();
const BUILT_IN_REFERENCE_LABELS_BY_LOCALE = new Map<Locale, Map<string, string>>();

function localizedTemplatesFor(locale: Locale): Record<string, BuiltInCanvasTemplateText> | null {
  return BUILT_IN_TEMPLATE_TEXT_BY_LOCALE[locale] ?? null;
}

function buildBuiltInTemplateLookup(locale: Locale, templates: Record<string, BuiltInCanvasTemplateText>) {
  const nodeTitlesByType = new Map<WorkflowNodeType, Map<string, string>>();
  const referenceLabels = new Map<string, string>();

  for (const [sourceLabel, localizedLabel] of Object.entries(DEFAULT_EXTERNAL_CONNECTION_LABELS[locale] ?? {})) {
    referenceLabels.set(sourceLabel, localizedLabel);
  }

  for (const [templateKey, item] of Object.entries(templates)) {
    const sourceTemplate = BUILT_IN_TEMPLATE_SOURCE_TEXT[templateKey];
    if (!sourceTemplate) {
      continue;
    }
    const allLocalizedTemplates = Object.values(BUILT_IN_TEMPLATE_TEXT_BY_LOCALE);
    for (const [nodeKey, title] of Object.entries(item.nodes)) {
      const sourceTitle = sourceTemplate.nodes[nodeKey];
      if (!sourceTitle) {
        continue;
      }
      const titles = nodeTitlesByType.get(sourceTitle.nodeType) ?? new Map<string, string>();
      titles.set(sourceTitle.title, title);
      for (const localizedTemplate of allLocalizedTemplates) {
        const localizedTitle = localizedTemplate[templateKey]?.nodes[nodeKey];
        if (localizedTitle) {
          titles.set(localizedTitle, title);
        }
      }
      nodeTitlesByType.set(sourceTitle.nodeType, titles);
    }
    for (const [nodeKey, sourceLabel] of Object.entries(sourceTemplate.outputSlots)) {
      const targetLabel = item.outputSlots[nodeKey] ?? sourceLabel;
      referenceLabels.set(sourceLabel, targetLabel);
      for (const localizedTemplate of allLocalizedTemplates) {
        const localizedLabel = localizedTemplate[templateKey]?.outputSlots[nodeKey];
        if (localizedLabel) {
          referenceLabels.set(localizedLabel, targetLabel);
        }
      }
    }
    for (const [nodeKey, sourceLabel] of Object.entries(sourceTemplate.referenceInputHints ?? {})) {
      const targetLabel = item.referenceInputHints?.[nodeKey] ?? sourceLabel;
      referenceLabels.set(sourceLabel, targetLabel);
      for (const localizedTemplate of allLocalizedTemplates) {
        const localizedLabel = localizedTemplate[templateKey]?.referenceInputHints?.[nodeKey];
        if (localizedLabel) {
          referenceLabels.set(localizedLabel, targetLabel);
        }
      }
    }
    for (const localizedLabel of Object.values({
      ...item.outputSlots,
      ...item.referenceInputHints,
    })) {
      referenceLabels.set(localizedLabel, localizedLabel);
    }
  }

  BUILT_IN_NODE_TITLE_BY_LOCALE.set(locale, nodeTitlesByType);
  BUILT_IN_REFERENCE_LABELS_BY_LOCALE.set(locale, referenceLabels);
}

for (const [locale, templates] of Object.entries(BUILT_IN_TEMPLATE_TEXT_BY_LOCALE) as Array<[
  Locale,
  Record<string, BuiltInCanvasTemplateText>,
]>) {
  buildBuiltInTemplateLookup(locale, templates);
}

function objectValue(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

function stringValue(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value.trim() : null;
}

function shouldLocalizeTemplate(template: CanvasTemplateSummary, locale: Locale): boolean {
  return locale !== DEFAULT_LOCALE && template.source === "builtin" && Boolean(localizedTemplatesFor(locale)?.[template.key]);
}

function localizedByKey(sourceValue: string, localizedValue: string | undefined, locale: Locale): string {
  return locale === DEFAULT_LOCALE ? sourceValue : (localizedValue ?? sourceValue);
}

export function localizeCanvasTemplateSummary(
  template: CanvasTemplateSummary,
  locale: Locale = DEFAULT_LOCALE,
): CanvasTemplateSummary {
  if (!shouldLocalizeTemplate(template, locale)) {
    return template;
  }
  const localized = localizedTemplatesFor(locale)?.[template.key];
  if (!localized) {
    return template;
  }
  return {
    ...template,
    title: localized.title,
    description: localized.description,
    scenario: {
      ...template.scenario,
      title: localized.scenarioTitle,
      description: localized.scenarioDescription,
    },
    preview_nodes: template.preview_nodes.map((node) => ({
      ...node,
      title: localizedByKey(node.title, localized.nodes[node.key], locale),
    })),
    output_slots: template.output_slots.map((slot) => ({
      ...slot,
      label: localizedByKey(slot.label, localized.outputSlots[slot.node_key], locale),
    })),
    reference_input_hints: template.reference_input_hints.map((hint) => ({
      ...hint,
      label: localizedByKey(hint.label, localized.referenceInputHints?.[hint.node_key], locale),
    })),
    default_external_connections: template.default_external_connections.map((connection) => ({
      ...connection,
      label: localizedByKey(
        connection.label,
        DEFAULT_EXTERNAL_CONNECTION_LABELS[locale]?.[connection.label],
        locale,
      ),
    })),
  };
}

export function localizeBuiltInTemplateNodeTitle(
  nodeType: WorkflowNodeType,
  title: string,
  locale: Locale = DEFAULT_LOCALE,
  configJson?: Record<string, unknown>,
): string | null {
  if (locale === DEFAULT_LOCALE) {
    return null;
  }
  const templateMetadata = objectValue(configJson?._canvas_template);
  const templateKey = stringValue(templateMetadata?.template_key);
  const nodeKey = stringValue(templateMetadata?.node_key);
  const localizedTemplates = localizedTemplatesFor(locale);
  const localizedByMetadata = templateKey && nodeKey ? localizedTemplates?.[templateKey]?.nodes[nodeKey] : null;
  const sourceNode = templateKey && nodeKey ? BUILT_IN_TEMPLATE_SOURCE_TEXT[templateKey]?.nodes[nodeKey] : null;
  const trimmed = title.trim();
  if (localizedByMetadata && sourceNode?.nodeType === nodeType && (trimmed === sourceNode.title || trimmed === localizedByMetadata)) {
    return localizedByMetadata;
  }
  if (!trimmed) {
    return null;
  }
  return BUILT_IN_NODE_TITLE_BY_LOCALE.get(locale)?.get(nodeType)?.get(trimmed) ?? null;
}

export function localizeBuiltInTemplateLabel(label: string, locale: Locale = DEFAULT_LOCALE): string | null {
  if (locale === DEFAULT_LOCALE) {
    return null;
  }
  const trimmed = label.trim();
  if (!trimmed) {
    return null;
  }
  return BUILT_IN_REFERENCE_LABELS_BY_LOCALE.get(locale)?.get(trimmed) ?? null;
}
