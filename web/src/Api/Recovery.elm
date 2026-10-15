module Api.Recovery exposing (list, verify)

import Api.Access exposing (authHeaders)
import Domain.Recovery exposing (Listing, Receipt, listingDecoder, receiptDecoder)
import Http
import String
import Url.Builder


list : String -> Maybe String -> (Result Http.Error Listing -> msg) -> Cmd msg
list apiBase credential toMsg =
    Http.request
        { method = "GET"
        , headers = authHeaders credential
        , url = endpoint apiBase [ "api", "recovery", "backups" ]
        , body = Http.emptyBody
        , expect = Http.expectJson toMsg listingDecoder
        , timeout = Just 15000
        , tracker = Nothing
        }


verify : String -> Maybe String -> String -> (Result Http.Error Receipt -> msg) -> Cmd msg
verify apiBase credential id toMsg =
    Http.request
        { method = "POST"
        , headers = authHeaders credential
        , url = endpoint apiBase [ "api", "recovery", "backups", id, "verify" ]
        , body = Http.emptyBody
        , expect = Http.expectJson toMsg receiptDecoder
        , timeout = Just 120000
        , tracker = Nothing
        }


endpoint : String -> List String -> String
endpoint apiBase path =
    if String.trim apiBase == "" then
        Url.Builder.absolute path []

    else
        Url.Builder.crossOrigin apiBase path []
