module Api.Error exposing (Failure(..), classify)

{-| Plain classification of Elm HTTP failures. Feature modules map these
facts to their own wording; domain logic can consume status facts without
depending on `Http`.
-}

import Http


type Failure
    = BadUrl String
    | Timeout
    | NetworkError
    | BadStatus Int
    | BadBody


classify : Http.Error -> Failure
classify error =
    case error of
        Http.BadUrl url ->
            BadUrl url

        Http.Timeout ->
            Timeout

        Http.NetworkError ->
            NetworkError

        Http.BadStatus status ->
            BadStatus status

        Http.BadBody _ ->
            BadBody
