import { describe, expect, it } from "vitest";

import type { PosterVariant, ProductDetail, ProductWorkflow, SourceAsset, WorkflowNode } from "../../lib/types";
import {
  buildPosterSourceAssetMap,
  getMaterializedPosterIdFromSourceAsset,
  getVisibleReferenceAssets,
} from "./galleryImages";

const createdAt = "2026-04-26T00:00:00Z";

function sourceAsset(overrides: Partial<SourceAsset>): SourceAsset {
  return {
    id: "asset-1",
    kind: "reference_image",
    original_filename: "reference.png",
    mime_type: "image/png",
    download_url: "/media/reference.png",
    preview_url: "/media/reference-preview.png",
    thumbnail_url: "/media/reference-thumb.png",
    created_at: createdAt,
    ...overrides,
  };
}

function poster(overrides: Partial<PosterVariant>): PosterVariant {
  return {
    id: "poster-1",
    product_id: "product-1",
    copy_set_id: "copy-1",
    kind: "promo_poster",
    template_name: "default",
    mime_type: "image/png",
    width: 1024,
    height: 1024,
    download_url: "/media/poster.png",
    preview_url: "/media/poster-preview.png",
    thumbnail_url: "/media/poster-thumb.png",
    created_at: createdAt,
    ...overrides,
  };
}

function product(sourceAssets: SourceAsset[]): ProductDetail {
  return {
    id: "product-1",
    name: "测试商品",
    category: null,
    price: null,
    source_note: null,
    workflow_state: "draft",
    source_assets: sourceAssets,
    latest_brief: null,
    current_confirmed_copy_set: null,
    copy_sets: [],
    poster_variants: [],
    recent_jobs: [],
    created_at: createdAt,
    updated_at: createdAt,
  };
}

function workflow(nodes: WorkflowNode[]): ProductWorkflow {
  return {
    id: "workflow-1",
    product_id: "product-1",
    title: "默认工作流",
    active: true,
    nodes,
    edges: [],
    runs: [],
    created_at: createdAt,
    updated_at: createdAt,
  };
}

function node(overrides: Partial<WorkflowNode>): WorkflowNode {
  return {
    id: "node-1",
    workflow_id: "workflow-1",
    node_type: "image_generation",
    title: "生图",
    position_x: 0,
    position_y: 0,
    config_json: {},
    status: "idle",
    output_json: null,
    failure_reason: null,
    last_run_at: null,
    created_at: createdAt,
    updated_at: createdAt,
    ...overrides,
  };
}

describe("product-detail gallery image helpers", () => {
  it("does not use legacy poster filename matching when explicit source_poster_variant_id is null", () => {
    const postersById = new Map([["poster-1", poster({ id: "poster-1" })]]);
    const asset = sourceAsset({
      original_filename: "poster-1.png",
      source_poster_variant_id: null,
    });

    expect(getMaterializedPosterIdFromSourceAsset(asset, postersById)).toBeNull();
  });

  it("maps generated posters to filled source assets from workflow output", () => {
    const mapping = buildPosterSourceAssetMap({
      product: product([]),
      workflow: workflow([
        node({
          output_json: {
            generated_poster_variant_ids: ["poster-1", "poster-2"],
            filled_source_asset_ids: ["asset-1", "asset-2"],
          },
        }),
      ]),
      posters: [poster({ id: "poster-1" }), poster({ id: "poster-2" })],
    });

    expect([...mapping.entries()]).toEqual([
      ["poster-1", "asset-1"],
      ["poster-2", "asset-2"],
    ]);
  });

  it("hides reference assets already represented by visible poster variants", () => {
    const sourceAssets = [
      sourceAsset({ id: "asset-from-poster", source_poster_variant_id: "poster-1" }),
      sourceAsset({ id: "manual-reference", original_filename: "manual.png" }),
    ];

    expect(
      getVisibleReferenceAssets({
        product: product(sourceAssets),
        posterSourceAssetIds: new Map([["poster-1", "asset-from-poster"]]),
        posters: [poster({ id: "poster-1" })],
      }).map((asset) => asset.id),
    ).toEqual(["manual-reference"]);
  });
});
