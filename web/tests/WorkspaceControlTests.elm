module WorkspaceControlTests exposing (suite)

import App.Keyboard as Keyboard
import App.Prefs as Prefs
import Domain.Query as Query
import Json.Decode as Decode


suite : List ( String, Bool )
suite =
    [ ( "sort preserves quoted tags and exclusions", Query.setOrder "score" "\"blue sky\" -tag:cat width:>=1200" == "\"blue sky\" -tag:cat width:>=1200 order:score" )
    , ( "sort replaces the previous order", Query.setOrder "score" "landscape order:newest" == "landscape order:score" )
    , ( "newest removes the score override", Query.setOrder "newest" "landscape order:score" == "landscape" )
    , ( "sort does not remove order text inside a quoted phrase", Query.setOrder "score" "\"order:score illustration\"" == "\"order:score illustration\" order:score" )
    , ( "score order reads the actual query", Query.order "tag:landscape order:score" == "score" )
    , ( "quoted order text does not change sort", Query.order "\"order:score\"" == "newest" )
    , ( "disclosure keys are handled as native controls", target "SUMMARY" == Just Keyboard.Control )
    , ( "select keys stay editable", target "SELECT" == Just Keyboard.Editable )
    , ( "video keys stay with playback controls", target "VIDEO" == Just Keyboard.MediaControl )
    , ( "audio keys stay with playback controls", target "AUDIO" == Just Keyboard.MediaControl )
    , ( "legacy preferences keep their settings and default to light", legacyPreferences )
    , ( "saved dark preference is restored", Decode.decodeString Prefs.decoder "{\"darkMode\":true}" |> Result.map .darkMode |> Result.withDefault False )
    ]


legacyPreferences : Bool
legacyPreferences =
    case Decode.decodeString Prefs.decoder "{\"thumbSize\":200,\"navigatorDocked\":false,\"savedSearches\":[\"tag:cat\"]}" of
        Ok prefs ->
            not prefs.darkMode && prefs.thumbSize == 200 && not prefs.navigatorDocked && prefs.savedSearches == [ "tag:cat" ]

        Err _ ->
            False


target : String -> Maybe Keyboard.Target
target tag =
    Decode.decodeString Keyboard.decoder
        ("{\"key\":\"Enter\",\"target\":{\"tagName\":\"" ++ tag ++ "\"}}")
        |> Result.toMaybe
        |> Maybe.map .target
