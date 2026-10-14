module Api.Ratings exposing (edit, encodeRequest, endpoint)

import Api.Access exposing (authHeaders)
import Domain.Post exposing (RatingResponse, RatingTarget, ratingResponseDecoder)
import Domain.Rating as Rating exposing (Rating)
import Http
import Json.Encode as Encode
import Url.Builder


endpoint : String -> String
endpoint apiBase =
    if String.trim apiBase == "" then
        Url.Builder.absolute [ "api", "posts", "ratings" ] []

    else
        Url.Builder.crossOrigin apiBase [ "api", "posts", "ratings" ] []


encodeTarget : RatingTarget -> Encode.Value
encodeTarget target =
    Encode.object
        [ ( "id", Encode.string target.id )
        , ( "version", Encode.int target.version )
        ]


{-| Every target version is encoded explicitly, including zero.
-}
encodeRequest : List RatingTarget -> Rating -> Encode.Value
encodeRequest targets rating =
    Encode.object
        [ ( "posts", Encode.list encodeTarget targets )
        , ( "rating", Encode.string (Rating.toString rating) )
        ]


edit : String -> Maybe String -> List RatingTarget -> Rating -> (Result Http.Error RatingResponse -> msg) -> Cmd msg
edit apiBase credential targets rating toMsg =
    Http.request
        { method = "POST"
        , headers = authHeaders credential
        , url = endpoint apiBase
        , body = Http.jsonBody (encodeRequest targets rating)
        , expect = Http.expectJson toMsg ratingResponseDecoder
        , timeout = Nothing
        , tracker = Nothing
        }
