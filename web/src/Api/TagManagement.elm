module Api.TagManagement exposing (apply, list, preview)

import Api.Access exposing (authHeaders)
import Domain.TagManagement as Domain exposing (ListPage, Mode, Preview, Receipt)
import Http
import Json.Encode as Encode
import Url.Builder


list : String -> Maybe String -> String -> Maybe String -> Int -> (Result Http.Error ListPage -> msg) -> Cmd msg
list apiBase credential prefix cursor limit toMsg =
    Http.request
        { method = "GET"
        , headers = authHeaders credential
        , url =
            endpoint apiBase
                [ "api", "tags", "manage" ]
                ([ Url.Builder.string "prefix" prefix, Url.Builder.int "limit" limit ]
                    ++ (cursor |> Maybe.map (Url.Builder.string "cursor") |> Maybe.map List.singleton |> Maybe.withDefault [])
                )
        , body = Http.emptyBody
        , expect = Http.expectJson toMsg Domain.listPageDecoder
        , timeout = Just 30000
        , tracker = Nothing
        }


preview : String -> Maybe String -> Mode -> String -> String -> (Result Http.Error Preview -> msg) -> Cmd msg
preview apiBase credential mode source target toMsg =
    Http.request
        { method = "POST"
        , headers = authHeaders credential
        , url = endpoint apiBase [ "api", "tags", "manage", "preview" ] []
        , body = Http.jsonBody (draftBody mode source target Nothing)
        , expect = Http.expectJson toMsg Domain.previewDecoder
        , timeout = Just 30000
        , tracker = Nothing
        }


apply : String -> Maybe String -> Mode -> String -> String -> Int -> (Result Http.Error Receipt -> msg) -> Cmd msg
apply apiBase credential mode source target expectedVersion toMsg =
    Http.request
        { method = "POST"
        , headers = authHeaders credential
        , url = endpoint apiBase [ "api", "tags", "manage", "apply" ] []
        , body = Http.jsonBody (draftBody mode source target (Just expectedVersion))
        , expect = Http.expectJson toMsg Domain.receiptDecoder
        , timeout = Just 30000
        , tracker = Nothing
        }


draftBody : Mode -> String -> String -> Maybe Int -> Encode.Value
draftBody mode source target expectedVersion =
    Encode.object
        ([ ( "mode", Encode.string (Domain.modeToString mode) )
         , ( "source_tag", Encode.string source )
         , ( "target_tag", Encode.string target )
         ]
            ++ (expectedVersion |> Maybe.map (\v -> [ ( "expected_version", Encode.int v ) ]) |> Maybe.withDefault [])
        )


endpoint : String -> List String -> List Url.Builder.QueryParameter -> String
endpoint apiBase path params =
    if String.trim apiBase == "" then
        Url.Builder.absolute path params

    else
        Url.Builder.crossOrigin apiBase path params
