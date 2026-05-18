package model

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

const (
	defaultBedrockEmbeddingModel   = "amazon.titan-embed-text-v2:0"
	defaultBedrockEmbeddingTimeout = 30 * time.Second
	bedrockEmbedPathFmt            = "/model/%s/invoke"
)

// BedrockEmbeddingConfig configures an AWS Bedrock embedding client.
type BedrockEmbeddingConfig struct {
	Region       string
	AccessKeyID  string
	SecretKey    string
	SessionToken string
	Model        string        // default: "amazon.titan-embed-text-v2:0"
	Timeout      time.Duration // default: 30s
}

// BedrockEmbeddingClient calls the AWS Bedrock runtime invoke API for embeddings.
// Supports models such as amazon.titan-embed-text-v2 and cohere.embed-english-v3.
type BedrockEmbeddingClient struct {
	region       string
	accessKeyID  string
	secretKey    string
	sessionToken string
	model        string
	client       *http.Client
}

func init() {
	RegisterEmbeddingClientBuilder(EmbedBedrock, func(input EmbeddingClientBuildInput) (agent.EmbeddingClient, error) {
		return NewBedrockEmbeddingClient(BedrockEmbeddingConfig{
			Model:   input.Model,
			Timeout: input.Timeout,
		})
	})
}

// NewBedrockEmbeddingClient creates a Bedrock embedding client. Credentials are
// resolved from the config first, then falling back to standard AWS environment
// variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN, AWS_REGION).
func NewBedrockEmbeddingClient(cfg BedrockEmbeddingConfig) (*BedrockEmbeddingClient, error) {
	region := normalize.FirstNonEmpty(cfg.Region, os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION"))
	if region == "" {
		return nil, fmt.Errorf("bedrock embedding: region is required (set BedrockEmbeddingConfig.Region or AWS_REGION)")
	}
	accessKeyID := normalize.FirstNonEmpty(cfg.AccessKeyID, os.Getenv("AWS_ACCESS_KEY_ID"))
	secretKey := normalize.FirstNonEmpty(cfg.SecretKey, os.Getenv("AWS_SECRET_ACCESS_KEY"))
	if accessKeyID == "" || secretKey == "" {
		return nil, fmt.Errorf("bedrock embedding: AWS credentials are required (set config or AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY)")
	}
	sessionToken := normalize.FirstNonEmpty(cfg.SessionToken, os.Getenv("AWS_SESSION_TOKEN"))
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = defaultBedrockEmbeddingModel
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultBedrockEmbeddingTimeout
	}
	return &BedrockEmbeddingClient{
		region:       region,
		accessKeyID:  accessKeyID,
		secretKey:    secretKey,
		sessionToken: sessionToken,
		model:        model,
		client:       &http.Client{Timeout: timeout},
	}, nil
}

// ---------------------------------------------------------------------------
// Wire types
// ---------------------------------------------------------------------------

// bedrockEmbedRequest is the JSON body for the Bedrock Titan embedding API.
type bedrockEmbedRequest struct {
	InputText string `json:"inputText"`
}

// bedrockEmbedResponse is the JSON response from the Bedrock Titan embedding API.
type bedrockEmbedResponse struct {
	Embedding []float32 `json:"embedding"`
}

// Embed generates vector embeddings for the given texts using the AWS Bedrock
// runtime invoke API. Texts are embedded one at a time since the Bedrock invoke
// endpoint processes a single input per call.
func (c *BedrockEmbeddingClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	results := make([][]float32, len(texts))
	for i, text := range texts {
		embedding, err := c.embedSingle(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("bedrock embedding: text %d: %w", i, err)
		}
		results[i] = embedding
	}
	return results, nil
}

func (c *BedrockEmbeddingClient) embedSingle(ctx context.Context, text string) ([]float32, error) {
	reqBody := bedrockEmbedRequest{InputText: text}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	endpoint := fmt.Sprintf("https://%s.%s.amazonaws.com"+bedrockEmbedPathFmt,
		bedrockService, c.region, c.model)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	if err := c.signRequest(httpReq, payload); err != nil {
		return nil, fmt.Errorf("sign request: %w", err)
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var embedResp bedrockEmbedResponse
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(embedResp.Embedding) == 0 {
		return nil, fmt.Errorf("API returned empty embedding")
	}

	return embedResp.Embedding, nil
}

// signRequest applies AWS Signature V4 to the given HTTP request.
// This reuses the same signing algorithm as BedrockClient.signRequest.
func (c *BedrockEmbeddingClient) signRequest(req *http.Request, payload []byte) error {
	now := time.Now().UTC()
	datestamp := now.Format(awsDateFormat)
	amzDate := now.Format(awsTimeFormat)

	req.Header.Set("X-Amz-Date", amzDate)
	if c.sessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", c.sessionToken)
	}

	host := req.URL.Host
	req.Header.Set("Host", host)

	payloadHash := sha256Hex(payload)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	// 1. Canonical request.
	signedHeaders, canonicalHeaders := buildCanonicalHeaders(req)
	canonicalURI := req.URL.Path
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalQueryString := req.URL.RawQuery

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQueryString,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	// 2. String to sign.
	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", datestamp, c.region, bedrockService)
	stringToSign := strings.Join([]string{
		awsSigV4Algo,
		amzDate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	// 3. Signing key.
	signingKey := deriveSigningKey(c.secretKey, datestamp, c.region, bedrockService)

	// 4. Signature.
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	// 5. Authorization header.
	authHeader := fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		awsSigV4Algo, c.accessKeyID, credentialScope, signedHeaders, signature)
	req.Header.Set("Authorization", authHeader)

	return nil
}
