# Home showcase provenance

- Web reference: `forma-reference.webp`
- Reference source bytes: `forma-reference.jpg` (PNG despite the historical extension)
- Source: [Perfume Bottle.jpg, Wikimedia Commons](https://commons.wikimedia.org/wiki/File:Perfume_Bottle.jpg)
- Author: ChickenFreak
- License: public domain, released by the copyright holder
- ProductFlow product: `18470ef4-d41a-4327-986f-4d9588198946`
- Workflow: `a98aa8a2-87c1-4312-96a8-0c628b00857a`
- Successful graph run: `c1a077db-960f-4530-9f53-3dd9fb349804`
- Homepage images: `forma-hero.webp`, `forma-scene.webp`, `forma-detail.webp`
- Generated source bytes: `forma-hero.jpg`, `forma-scene.jpg`, `forma-detail.jpg` (PNG despite the historical extensions)
- Historical product UI captures, no longer rendered on Home: `ui-create.png`, `ui-workbench.png`, `ui-image-chat.png`, `ui-library.png`
- Media library assets: `f2a0befd-cfb8-4c33-a363-00d87f1a6615`, `311aa7c3-597b-46ee-98a6-892b81fab69f`, `ee7a4bac-cce6-44d2-803e-83df823df959`
- Image iteration session: `4550d9be-6818-4a35-9ca6-ea634c36b96b`

The generated images were downloaded from the ProductFlow product library after the graph run reached `succeeded`. The homepage serves WebP derivatives encoded at quality 0.86 with the original pixel dimensions and no content changes. The four historical `.jpg` files contain PNG bytes and remain as local conversion sources; Home does not request them. The three displayed WebP outputs total approximately 180 KiB, down from approximately 5.2 MiB of source images.

The UI captures record the local ProductFlow pages at capture time and do not document the current UI. The three successful graph outputs were collected into the global media library before `ui-library.png` was captured. `ui-image-chat.png` records the image iteration session while its provider task is still running; it is not a completed generation.

Home uses an accessible preview dialog to switch between these outputs and the source reference. It does not simulate a workflow run. Browser coverage: `web/e2e/home.spec.ts` (three viewport sizes, four locales, light/dark themes, reference switching, image navigation, focus trapping and restoration).
