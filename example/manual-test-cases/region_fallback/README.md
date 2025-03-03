# Region Fallback Test Cases

This directory contains test configurations for the region fallback feature in NotDiamond.

## Overview

Region fallback allows you to specify multiple regions for a model and automatically fall back to alternative regions if the primary region fails. This is useful for:

- Improving reliability by having backup regions
- Handling region-specific outages

## Implementation Details

The region fallback feature has been implemented with the following key components:

1. **Model Naming Convention**: Models can now include a region in their name using the format `provider/model/region` (e.g., `vertex/gemini-pro/us-east4`).

2. **Region Prioritization**:

   - If a region is specified in the request, that specific region is tried first.

3. **Fallback Mechanism**:
   - If a request to a specific region fails, the system automatically tries the next region in the list.
   - Error tracking and health checks are performed for each region.

## Test Cases

1. **Basic Region Fallback** (`1_region_fallback.go`)

   - Simple ordered fallback for Vertex AI, OpenAI, and Azure
   - Mixed provider fallback

2. **Weighted Region Fallback** (`2_region_fallback_weighted.go`)

   - Weighted distribution across regions
   - Region fallback with timeouts

3. **Error Tracking and Retries** (`3_region_fallback_error_tracking.go`)
   - Region fallback with error tracking
   - Status code-specific retry configuration
   - Backoff between retries

## Region Support by Provider

### Vertex AI

- Supports all Google Cloud regions where Vertex AI is available
- Common regions: `us-central1`, `us-east4`, `us-west1`, `europe-west4`, etc.

### OpenAI

- Not supported

### Azure OpenAI

- Defined in AzureRegions map in Config struct

## Usage

To use these test cases, modify the example application to use one of these configurations:
