module Api.Access exposing
    ( Capabilities
    , authHeaderValue
    , authHeaders
    , capabilitiesDecoder
    , discover
    , endpoint
    , validate
    )

{-| Write-capability discovery against `GET /api/capabilities`.

`discover` carries no credential and reports whether the deployment gates
writes. `validate` sends one candidate token for a non-mutating capability
check; the server never returns the token or a digest of it. The shared
`authHeaders` helper keeps every mutation family on the same
`Authorization: Bearer <token>` selection: a missing or blank credential
sends no header at all, so public reads stay uncredentialed and locked
browsers never leak an empty bearer.
-}

import Http
import Json.Decode as Decode exposing (Decoder)
import String
import Url.Builder


type alias Capabilities =
    { writesRequireAuth : Bool
    , canWrite : Bool
    , actor : Maybe String
    }


capabilitiesDecoder : Decoder Capabilities
capabilitiesDecoder =
    Decode.map3 Capabilities
        (Decode.field "writes_require_auth" Decode.bool)
        (Decode.field "can_write" Decode.bool)
        (Decode.field "actor" (Decode.nullable Decode.string))


endpoint : String -> String
endpoint apiBase =
    if String.trim apiBase == "" then
        Url.Builder.absolute [ "api", "capabilities" ] []

    else
        Url.Builder.crossOrigin apiBase [ "api", "capabilities" ] []


{-| Capability discovery without credentials. Never 401s on its own: with
no `Authorization` header the server reports the gate status as JSON.
-}
discover : String -> (Result Http.Error Capabilities -> msg) -> Cmd msg
discover apiBase toMsg =
    Http.get { url = endpoint apiBase, expect = Http.expectJson toMsg capabilitiesDecoder }


{-| Non-mutating validation of one candidate token. A wrong token comes
back as `401`; the candidate itself never appears in any response body.
-}
validate : String -> String -> (Result Http.Error Capabilities -> msg) -> Cmd msg
validate apiBase candidate toMsg =
    Http.request
        { method = "GET"
        , headers = authHeaders (Just candidate)
        , url = endpoint apiBase
        , body = Http.emptyBody
        , expect = Http.expectJson toMsg capabilitiesDecoder
        , timeout = Nothing
        , tracker = Nothing
        }


{-| Header value for a credential, or `Nothing` when there is nothing worth
sending. Blank tokens collapse to `Nothing` so a cleared draft can never
produce `Authorization: Bearer ` on the wire.
-}
authHeaderValue : Maybe String -> Maybe String
authHeaderValue candidate =
    case candidate of
        Nothing ->
            Nothing

        Just raw ->
            let
                trimmed =
                    String.trim raw
            in
            if trimmed == "" then
                Nothing

            else
                Just ("Bearer " ++ trimmed)


{-| Zero or one `Authorization` headers for a credential. Public reads pass
`Nothing`; mutations pass the unlocked token.
-}
authHeaders : Maybe String -> List Http.Header
authHeaders candidate =
    case authHeaderValue candidate of
        Nothing ->
            []

        Just value ->
            [ Http.header "Authorization" value ]
